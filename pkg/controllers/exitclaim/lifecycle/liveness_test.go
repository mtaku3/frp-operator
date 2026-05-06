package lifecycle_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
)

// readyClaim returns a Launched + Registered + Ready claim with the
// given PublicIP wired so the Liveness probe can reach the test
// server. Lifecycle phase chain only invokes Liveness post-Initialize,
// so all four conditions are required.
func readyClaim(name, ip string) *v1alpha1.ExitClaim {
	c := newClaim(name)
	c.Status.PublicIP = ip
	now := metav1.Now()
	c.Status.Conditions = []metav1.Condition{
		{Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: now},
		{Type: v1alpha1.ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonReconciled, LastTransitionTime: now},
		{Type: v1alpha1.ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonReconciled, LastTransitionTime: now},
		{Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonReconciled, LastTransitionTime: now},
	}
	return c
}

// TestLiveness_PostReady_HealthyClearsCounter: a Ready claim with a
// previously-recorded failure counter clears the annotation on the
// next successful probe.
func TestLiveness_PostReady_HealthyClearsCounter(t *testing.T) {
	mustAddSchemes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.68.1"}`))
	}))
	defer srv.Close()

	c := readyClaim("e1", "203.0.113.10")
	c.Annotations = map[string]string{lifecycle.AnnotationLivenessFailures: "2"}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(c).WithStatusSubresource(c).Build()
	l := &lifecycle.Liveness{
		KubeClient:   cl,
		AdminFactory: func(_ string) *admin.Client { return admin.New(srv.URL) },
	}
	if _, err := l.Reconcile(context.Background(), c); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got v1alpha1.ExitClaim
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got)
	if v, ok := got.Annotations[lifecycle.AnnotationLivenessFailures]; ok {
		t.Fatalf("expected liveness-failures annotation cleared, got %q", v)
	}
}

// TestLiveness_PostReady_FailureIncrementsCounter: probe failure
// increments the annotation; below threshold no Disrupted=True.
func TestLiveness_PostReady_FailureIncrementsCounter(t *testing.T) {
	mustAddSchemes(t)
	// Server that always 500s simulates an unreachable admin API.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := readyClaim("e1", "203.0.113.10")
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(c).WithStatusSubresource(c).Build()
	l := &lifecycle.Liveness{
		KubeClient:       cl,
		AdminFactory:     func(_ string) *admin.Client { return admin.New(srv.URL) },
		FailureThreshold: 3,
	}
	if _, err := l.Reconcile(context.Background(), c); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got v1alpha1.ExitClaim
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got)
	if got.Annotations[lifecycle.AnnotationLivenessFailures] != "1" {
		t.Fatalf("expected counter=1, got %q", got.Annotations[lifecycle.AnnotationLivenessFailures])
	}
	for _, cond := range got.Status.Conditions {
		if cond.Type == v1alpha1.ConditionTypeDisrupted && cond.Status == metav1.ConditionTrue {
			t.Fatal("Disrupted=True before threshold")
		}
	}
}

// TestLiveness_PostReady_ThresholdMarksDisrupted: when the failure
// counter reaches FailureThreshold the claim is stamped Ready=False
// + Disrupted=True so the disruption queue picks it up.
func TestLiveness_PostReady_ThresholdMarksDisrupted(t *testing.T) {
	mustAddSchemes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := readyClaim("e1", "203.0.113.10")
	c.Annotations = map[string]string{lifecycle.AnnotationLivenessFailures: "2"}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(c).WithStatusSubresource(c).Build()
	l := &lifecycle.Liveness{
		KubeClient:       cl,
		AdminFactory:     func(_ string) *admin.Client { return admin.New(srv.URL) },
		FailureThreshold: 3,
	}
	if _, err := l.Reconcile(context.Background(), c); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var got v1alpha1.ExitClaim
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "e1"}, &got)
	if got.Annotations[lifecycle.AnnotationLivenessFailures] != "3" {
		t.Fatalf("expected counter=3, got %q", got.Annotations[lifecycle.AnnotationLivenessFailures])
	}
	gotDisrupted := false
	gotReadyFalse := false
	for _, cond := range got.Status.Conditions {
		if cond.Type == v1alpha1.ConditionTypeDisrupted && cond.Status == metav1.ConditionTrue {
			gotDisrupted = true
		}
		if cond.Type == v1alpha1.ConditionTypeReady && cond.Status == metav1.ConditionFalse {
			gotReadyFalse = true
		}
	}
	if !gotDisrupted {
		t.Fatal("expected Disrupted=True at threshold")
	}
	if !gotReadyFalse {
		t.Fatal("expected Ready=False at threshold")
	}
}
