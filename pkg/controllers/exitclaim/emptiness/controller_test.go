package emptiness_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/emptiness"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return s
}

//nolint:unparam // name kept variadic for future test cases
func readyClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: "fake://" + name,
			Conditions: []metav1.Condition{{
				Type:               v1alpha1.ConditionTypeReady,
				Status:             metav1.ConditionTrue,
				Reason:             "Ok",
				Message:            "ok",
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

func boundTunnel(name, claimName string) *v1alpha1.Tunnel {
	return &v1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Status:     v1alpha1.TunnelStatus{AssignedExit: claimName},
	}
}

//nolint:unparam // type param kept generic for future condition assertions
func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

func reconcile(t *testing.T, c *emptiness.Controller, name string) {
	t.Helper()
	if _, err := c.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func TestEmptyTrue_NoBoundTunnels(t *testing.T) {
	claim := readyClaim("e1")
	kc := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&v1alpha1.ExitClaim{}).
		WithObjects(claim).Build()

	c := &emptiness.Controller{Client: kc}
	reconcile(t, c, "e1")

	var got v1alpha1.ExitClaim
	if err := kc.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	cond := findCondition(got.Status.Conditions, v1alpha1.ConditionTypeEmpty)
	if cond == nil {
		t.Fatal("expected Empty condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Empty=True, got %s", cond.Status)
	}
	if cond.LastTransitionTime.IsZero() {
		t.Fatal("expected non-zero LastTransitionTime")
	}
}

func TestEmptyFalse_TunnelBound(t *testing.T) {
	claim := readyClaim("e1")
	tun := boundTunnel("t1", "e1")
	kc := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&v1alpha1.ExitClaim{}).
		WithObjects(claim, tun).Build()

	c := &emptiness.Controller{Client: kc}
	reconcile(t, c, "e1")

	var got v1alpha1.ExitClaim
	_ = kc.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got)
	cond := findCondition(got.Status.Conditions, v1alpha1.ConditionTypeEmpty)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Empty=False, got %v", cond)
	}
}

// TestEmptyUnknown_PreReady covers the karpenter convention: emptiness
// is only meaningful post-Ready. A claim still launching gets
// Empty=Unknown so disruption.candidates won't treat it as drainable.
func TestEmptyUnknown_PreReady(t *testing.T) {
	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "e2"},
		// no Ready condition
	}
	kc := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&v1alpha1.ExitClaim{}).
		WithObjects(claim).Build()

	c := &emptiness.Controller{Client: kc}
	reconcile(t, c, "e2")

	var got v1alpha1.ExitClaim
	_ = kc.Get(context.Background(), types.NamespacedName{Name: "e2"}, &got)
	cond := findCondition(got.Status.Conditions, v1alpha1.ConditionTypeEmpty)
	if cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Fatalf("expected Empty=Unknown pre-Ready, got %v", cond)
	}
}

// TestEmptyTransition_PreservesLastTransitionTime: re-reconciling a
// still-empty claim must not bump LastTransitionTime — that timestamp
// is what disruption uses as the since-when stamp for ConsolidateAfter.
func TestEmptyTransition_PreservesLastTransitionTime(t *testing.T) {
	claim := readyClaim("e1")
	kc := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&v1alpha1.ExitClaim{}).
		WithObjects(claim).Build()

	c := &emptiness.Controller{Client: kc}
	reconcile(t, c, "e1")

	var first v1alpha1.ExitClaim
	_ = kc.Get(context.Background(), types.NamespacedName{Name: "e1"}, &first)
	stamp := findCondition(first.Status.Conditions, v1alpha1.ConditionTypeEmpty).LastTransitionTime

	// Reconcile again — same state, no transition.
	reconcile(t, c, "e1")
	var second v1alpha1.ExitClaim
	_ = kc.Get(context.Background(), types.NamespacedName{Name: "e1"}, &second)
	got := findCondition(second.Status.Conditions, v1alpha1.ConditionTypeEmpty).LastTransitionTime
	if !got.Equal(&stamp) {
		t.Fatalf("LastTransitionTime changed without status change: %v vs %v", stamp, got)
	}
}

// TestEmptyTransition_FlipsStatus verifies that adding a bound tunnel
// flips the condition from True → False. Timestamp progression is a
// production property (`metav1.Now()` is called at every patch when
// status changes); the fake client's RFC3339 second-resolution
// roundtrip makes a strict timestamp inequality assertion flaky in
// unit tests, so we only assert status here.
func TestEmptyTransition_FlipsStatus(t *testing.T) {
	claim := readyClaim("e1")
	kc := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&v1alpha1.ExitClaim{}).
		WithObjects(claim).Build()

	c := &emptiness.Controller{Client: kc}
	reconcile(t, c, "e1") // Empty=True

	tun := boundTunnel("t1", "e1")
	if err := kc.Create(context.Background(), tun); err != nil {
		t.Fatalf("create tunnel: %v", err)
	}
	tun.Status.AssignedExit = "e1"
	_ = kc.Status().Update(context.Background(), tun)

	reconcile(t, c, "e1") // Empty=False expected

	var got v1alpha1.ExitClaim
	_ = kc.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got)
	cond := findCondition(got.Status.Conditions, v1alpha1.ConditionTypeEmpty)
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Empty=False, got %s", cond.Status)
	}
}
