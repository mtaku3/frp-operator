package lifecycle_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
)

func TestInitializer_MarksReady(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("i1")
	now := metav1.Now()
	claim.Status.Conditions = []metav1.Condition{
		{Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: now},
		{Type: v1alpha1.ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonReconciled, LastTransitionTime: now},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	i := &lifecycle.Initializer{KubeClient: cl}
	if _, err := i.Reconcile(context.Background(), claim); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if c := findCond(claim, v1alpha1.ConditionTypeInitialized); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("Initialized != True: %+v", c)
	}
	if c := findCond(claim, v1alpha1.ConditionTypeReady); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("Ready != True: %+v", c)
	}
}

func TestInitializer_RegisteredFalse(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("i2")
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	i := &lifecycle.Initializer{KubeClient: cl}
	if _, err := i.Reconcile(context.Background(), claim); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if findCond(claim, v1alpha1.ConditionTypeReady) != nil {
		t.Errorf("should not be Ready when not Registered")
	}
}

func TestLiveness_TTLExceededDeletes(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("l1")
	long := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	claim.Status.Conditions = []metav1.Condition{
		{Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: long},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	l := &lifecycle.Liveness{KubeClient: cl}
	if _, err := l.Reconcile(context.Background(), claim); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// The claim should now be deleted (or at least flagged for deletion).
	got := &v1alpha1.ExitClaim{}
	err := cl.Get(context.Background(), types.NamespacedName{Name: "l1"}, got)
	if err == nil && got.DeletionTimestamp.IsZero() {
		t.Errorf("expected deletion of stale claim")
	}
}

func TestLiveness_WithinTTLRequeues(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("l2")
	now := metav1.Now()
	claim.Status.Conditions = []metav1.Condition{
		{Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: now},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	l := &lifecycle.Liveness{KubeClient: cl}
	res, err := l.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter; got %+v", res)
	}
}

func TestLiveness_RegisteredSkips(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("l3")
	long := metav1.NewTime(time.Now().Add(-time.Hour))
	claim.Status.Conditions = []metav1.Condition{
		{Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: long},
		{Type: v1alpha1.ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonReconciled, LastTransitionTime: long},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	l := &lifecycle.Liveness{KubeClient: cl}
	res, err := l.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 0 || res.Requeue {
		t.Errorf("expected no-op; got %+v", res)
	}
}
