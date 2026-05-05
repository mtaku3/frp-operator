package lifecycle_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
)

func TestRegistrar_Reachable(t *testing.T) {
	mustAddSchemes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.99.0","bindPort":7000}`))
	}))
	defer srv.Close()

	claim := newClaim("r1")
	claim.Status.PublicIP = "203.0.113.1"
	claim.Status.Conditions = []metav1.Condition{{
		Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue,
		Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	r := &lifecycle.Registrar{
		KubeClient: cl,
		AdminFactory: func(_ string) *admin.Client {
			// Redirect all calls to the httptest server.
			return admin.New(srv.URL)
		},
	}
	if _, err := r.Reconcile(context.Background(), claim); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	c := findCond(claim, v1alpha1.ConditionTypeRegistered)
	if c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected Registered=True; got %+v", c)
	}
	if claim.Status.FrpsVersion != "v0.99.0" {
		t.Errorf("FrpsVersion not stamped: %q", claim.Status.FrpsVersion)
	}
}

func TestRegistrar_Unreachable(t *testing.T) {
	mustAddSchemes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	claim := newClaim("r2")
	claim.Status.PublicIP = "203.0.113.2"
	claim.Status.Conditions = []metav1.Condition{{
		Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue,
		Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	r := &lifecycle.Registrar{
		KubeClient:   cl,
		AdminFactory: func(_ string) *admin.Client { return admin.New(srv.URL) },
	}
	res, err := r.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter, got %+v", res)
	}
	c := findCond(claim, v1alpha1.ConditionTypeRegistered)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != v1alpha1.ReasonAdminAPIUnreachable {
		t.Errorf("expected Registered=False/AdminAPIUnreachable, got %+v", c)
	}
}

func TestRegistrar_NoPublicIP(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("r3")
	claim.Status.Conditions = []metav1.Condition{{
		Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue,
		Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	r := &lifecycle.Registrar{KubeClient: cl}
	res, err := r.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter, got %+v", res)
	}
	if findCond(claim, v1alpha1.ConditionTypeRegistered) != nil {
		t.Errorf("should not have set Registered yet")
	}
}

func TestRegistrar_NotLaunched(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("r4")
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	r := &lifecycle.Registrar{KubeClient: cl}
	res, err := r.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !res.IsZero() {
		t.Errorf("expected no-op, got %+v", res)
	}
}
