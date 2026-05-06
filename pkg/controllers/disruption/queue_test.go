package disruption_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

type fakeProvisioner struct {
	created      []*v1alpha1.ExitClaim
	err          error
	kube         client.Client
	beforeCreate func()
	skipCreate   bool
}

func (f *fakeProvisioner) CreateReplacements(ctx context.Context, claims []*v1alpha1.ExitClaim) error {
	if f.beforeCreate != nil {
		f.beforeCreate()
	}
	if f.err != nil {
		return f.err
	}
	if f.skipCreate {
		f.created = append(f.created, claims...)
		return nil
	}
	for _, c := range claims {
		c.Name = "replacement-" + randomNameSuffix()
		if err := f.kube.Create(ctx, c); err != nil {
			return err
		}
		f.created = append(f.created, c)
	}
	return nil
}

func randomNameSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	now := time.Now().UnixNano()
	out := make([]byte, 6)
	for i := range out {
		out[i] = charset[now%26]
		now /= 26
	}
	return string(out)
}

func newQueueTestSetup(t *testing.T, claims ...*v1alpha1.ExitClaim) (*disruption.Queue, client.Client, *state.Cluster, *fakeProvisioner) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	objs := make([]runtime.Object, 0, len(claims))
	for _, c := range claims {
		objs = append(objs, c)
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&v1alpha1.ExitClaim{}, &v1alpha1.Tunnel{}).
		Build()
	cluster := state.NewCluster(kube)
	for _, c := range claims {
		cluster.UpdateExit(c)
	}
	prov := &fakeProvisioner{kube: kube}
	q := disruption.NewQueue(kube, cluster, prov)
	q.ReplacementPollInterval = 10 * time.Millisecond
	q.ReplacementReadyTimeout = 2 * time.Second
	return q, kube, cluster, prov
}

func TestQueue_DeletesCandidateWithoutReplacements(t *testing.T) {
	claim := candReadyClaim("e1")
	// Finalizer keeps the object visible after Delete so we can observe
	// DeletionTimestamp.
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	q, kube, cluster, _ := newQueueTestSetup(t, claim)
	cmd := &disruption.Command{
		Reason: v1alpha1.DisruptionReasonEmpty,
		Method: "Emptiness",
		Candidates: []*disruption.Candidate{{
			Claim: claim,
			State: cluster.ExitForName("e1"),
			Pool:  &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p1"}},
		}},
	}
	if err := q.Enqueue(context.Background(), cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if !cluster.ExitForName("e1").IsMarkedForDeletion() {
		t.Fatal("candidate should be MarkedForDeletion")
	}
	var live v1alpha1.ExitClaim
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live); err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if live.DeletionTimestamp == nil {
		t.Fatal("expected DeletionTimestamp set")
	}
}

// TestQueue_CordonsBeforeReplacements verifies that the queue stamps
// Disrupted=True on every candidate BEFORE asking the provisioner to
// launch replacements. This is the cordon gate the scheduler reads
// during the replacement-Ready wait window.
func TestQueue_CordonsBeforeReplacements(t *testing.T) {
	claim := candReadyClaim("e1")
	claim.Finalizers = []string{v1alpha1.TerminationFinalizer}
	q, kube, cluster, prov := newQueueTestSetup(t, claim)

	// Trap the moment the provisioner is asked to create replacements;
	// at that moment the candidate must already carry Disrupted=True.
	prov.beforeCreate = func() {
		var live v1alpha1.ExitClaim
		_ = kube.Get(context.Background(), types.NamespacedName{Name: "e1"}, &live)
		if !disruptedConditionTrue(live.Status.Conditions) {
			t.Fatal("candidate should be cordoned (Disrupted=True) before CreateReplacements")
		}
	}

	replacement := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "r1"},
		Status: v1alpha1.ExitClaimStatus{
			Conditions: []metav1.Condition{{
				Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue,
				Reason: "Ok", Message: "ok", LastTransitionTime: metav1.Now(),
			}},
		},
	}

	cmd := &disruption.Command{
		Reason: v1alpha1.DisruptionReasonDrifted,
		Method: "Drift",
		Candidates: []*disruption.Candidate{{
			Claim: claim,
			State: cluster.ExitForName("e1"),
			Pool:  &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p1"}},
		}},
		Replacements: []*v1alpha1.ExitClaim{replacement},
	}

	// Pre-create the replacement Ready so wait returns immediately;
	// fakeProvisioner.CreateReplacements doesn't auto-mark Ready.
	if err := kube.Create(context.Background(), replacement); err != nil {
		t.Fatalf("seed replacement: %v", err)
	}
	_ = kube.Status().Update(context.Background(), replacement)

	prov.skipCreate = true // replacement already exists
	if err := q.Enqueue(context.Background(), cmd); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func disruptedConditionTrue(conds []metav1.Condition) bool {
	for _, c := range conds {
		if c.Type == v1alpha1.ConditionTypeDisrupted && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func TestQueue_RequiresProvisionerForReplacements(t *testing.T) {
	claim := candReadyClaim("e1")
	q, _, cluster, _ := newQueueTestSetup(t, claim)
	q.Provisioner = nil
	cmd := &disruption.Command{
		Reason: v1alpha1.DisruptionReasonDrifted,
		Candidates: []*disruption.Candidate{{
			Claim: claim,
			State: cluster.ExitForName("e1"),
		}},
		Replacements: []*v1alpha1.ExitClaim{{ObjectMeta: metav1.ObjectMeta{GenerateName: "r-"}}},
	}
	err := q.Enqueue(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error when replacements requested without provisioner")
	}
}

func TestQueue_PropagatesProvisionerError(t *testing.T) {
	claim := candReadyClaim("e1")
	q, _, cluster, prov := newQueueTestSetup(t, claim)
	prov.err = errors.New("boom")
	cmd := &disruption.Command{
		Candidates: []*disruption.Candidate{{
			Claim: claim,
			State: cluster.ExitForName("e1"),
		}},
		Replacements: []*v1alpha1.ExitClaim{{ObjectMeta: metav1.ObjectMeta{GenerateName: "r-"}}},
	}
	if err := q.Enqueue(context.Background(), cmd); err == nil {
		t.Fatal("expected provisioner error to propagate")
	}
}
