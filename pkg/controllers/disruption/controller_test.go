package disruption_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption/methods"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// envtest-equivalent harness, but driven via the controller-runtime fake
// client. It exercises the same Synced→GetCandidates→Method→Validate→Queue
// orchestration as the real controller.

func newControllerHarness(t *testing.T, claims []*v1alpha1.ExitClaim, pools []*v1alpha1.ExitPool, ms []disruption.Method) (*disruption.Controller, client.Client, *state.Cluster) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	objs := make([]runtime.Object, 0, len(claims)+len(pools))
	for _, c := range claims {
		objs = append(objs, c)
	}
	for _, p := range pools {
		objs = append(objs, p)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).
		WithStatusSubresource(&v1alpha1.ExitClaim{}, &v1alpha1.Tunnel{}).Build()
	cluster := state.NewCluster(kube)
	for _, p := range pools {
		cluster.UpdatePool(p)
	}
	for _, c := range claims {
		cluster.UpdateExit(c)
	}
	q := disruption.NewQueue(kube, cluster, &fakeProvisioner{kube: kube})
	q.ReplacementPollInterval = 5 * time.Millisecond
	q.ReplacementReadyTimeout = 200 * time.Millisecond
	ctrlr := disruption.New(cluster, kube, q, ms)
	return ctrlr, kube, cluster
}

func TestController_EmptyExitGetsDeleted(t *testing.T) {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Minute}},
		},
	}
	claim := candReadyClaim("e1")
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	// Backdate creation so the synthetic LastBindingChange (CreationTimestamp
	// fallback) is older than the pool's ConsolidateAfter.
	claim.CreationTimestamp = metav1.NewTime(time.Now().Add(-10 * time.Minute))
	emptiness := methods.NewEmptiness()
	ctrlr, kube, cluster := newControllerHarness(t,
		[]*v1alpha1.ExitClaim{claim},
		[]*v1alpha1.ExitPool{pool},
		[]disruption.Method{emptiness},
	)
	if _, err := ctrlr.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !cluster.ExitForName("e1").IsMarkedForDeletion() {
		t.Fatal("empty exit should be marked for deletion")
	}
	var live v1alpha1.ExitClaim
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live); err != nil {
		t.Fatalf("get e1: %v", err)
	}
	if live.DeletionTimestamp == nil {
		t.Fatal("expected DeletionTimestamp on e1")
	}
}

func TestController_BudgetZeroBlocksEmpty(t *testing.T) {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{
				ConsolidateAfter: v1alpha1.Duration{Duration: time.Minute},
				Budgets: []v1alpha1.DisruptionBudget{{
					Nodes:   "0",
					Reasons: []v1alpha1.DisruptionReason{v1alpha1.DisruptionReasonEmpty},
				}},
			},
		},
	}
	claim := candReadyClaim("e1")
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	claim.CreationTimestamp = metav1.NewTime(time.Now().Add(-10 * time.Minute))
	ctrlr, kube, cluster := newControllerHarness(t,
		[]*v1alpha1.ExitClaim{claim},
		[]*v1alpha1.ExitPool{pool},
		[]disruption.Method{methods.NewEmptiness()},
	)
	if _, err := ctrlr.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if cluster.ExitForName("e1").IsMarkedForDeletion() {
		t.Fatal("budget=0 should block emptiness disruption")
	}
	var live v1alpha1.ExitClaim
	_ = kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live)
	if live.DeletionTimestamp != nil {
		t.Fatal("budget=0 should not delete claim")
	}
}

func TestController_ExpirationBypassesBudget(t *testing.T) {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{
				Budgets: []v1alpha1.DisruptionBudget{{Nodes: "0"}},
			},
		},
	}
	claim := candReadyClaim("e1")
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	claim.CreationTimestamp = metav1.NewTime(time.Now().Add(-10 * time.Hour))
	claim.Spec.ExpireAfter = v1alpha1.Duration{Duration: time.Hour}
	// Provisioner adapter gets called for replacement creation; the queue
	// then waits for them to reach Ready. The fake provisioner creates
	// claims; we hydrate Ready immediately via a status patch in a
	// goroutine to satisfy the wait.
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(claim, pool).
		WithStatusSubresource(&v1alpha1.ExitClaim{}, &v1alpha1.Tunnel{}).Build()
	cluster := state.NewCluster(kube)
	cluster.UpdatePool(pool)
	cluster.UpdateExit(claim)

	prov := &readyMakingProvisioner{kube: kube}
	q := disruption.NewQueue(kube, cluster, prov)
	q.ReplacementPollInterval = 10 * time.Millisecond
	q.ReplacementReadyTimeout = 1 * time.Second
	ctrlr := disruption.New(cluster, kube, q, []disruption.Method{methods.NewExpiration()})

	if _, err := ctrlr.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var live v1alpha1.ExitClaim
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live); err != nil {
		t.Fatalf("get: %v", err)
	}
	if live.DeletionTimestamp == nil {
		t.Fatal("expiration must delete the expired claim despite budget=0")
	}
}

// readyMakingProvisioner creates each replacement claim and immediately marks
// it Ready=True so the queue's wait loop completes inside the test.
type readyMakingProvisioner struct {
	kube client.Client
}

func (r *readyMakingProvisioner) CreateReplacements(ctx context.Context, claims []*v1alpha1.ExitClaim) error {
	for _, c := range claims {
		if c.Name == "" {
			c.Name = "repl-" + randomNameSuffix()
		}
		if err := r.kube.Create(ctx, c); err != nil {
			return err
		}
		patch := client.MergeFrom(c.DeepCopy())
		c.Status.Conditions = []metav1.Condition{{
			Type:               v1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Ok",
			LastTransitionTime: metav1.Now(),
		}}
		if err := r.kube.Status().Patch(ctx, c, patch); err != nil {
			return err
		}
	}
	return nil
}

func TestController_BoundExitNotEmptyCandidate(t *testing.T) {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Second}},
		},
	}
	claim := candReadyClaim("e1")
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	claim.CreationTimestamp = metav1.NewTime(time.Now().Add(-1 * time.Hour))
	ctrlr, kube, cluster := newControllerHarness(t,
		[]*v1alpha1.ExitClaim{claim},
		[]*v1alpha1.ExitPool{pool},
		[]disruption.Method{methods.NewEmptiness()},
	)
	// Bind a tunnel onto e1 so it isn't empty.
	cluster.UpdateTunnelBinding(state.TunnelKey("ns/t1"), "e1", []int32{80})

	if _, err := ctrlr.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var live v1alpha1.ExitClaim
	_ = kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live)
	if live.DeletionTimestamp != nil {
		t.Fatal("non-empty exit must not be deleted by Emptiness")
	}
}
