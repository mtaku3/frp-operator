package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	cpfake "github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
)

func mustAddSchemes(t *testing.T) {
	t.Helper()
	if err := v1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
}

func newClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{
				Group: "frp.operator.io",
				Kind:  "FakeProviderClass",
				Name:  "default",
			},
			Frps: v1alpha1.FrpsConfig{
				Version:    "v0.68.1",
				BindPort:   7000,
				AdminPort:  7400,
				AllowPorts: []string{"1024-65535"},
				Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
			},
		},
	}
}

func TestLauncher_Success(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("c1")
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	registry := cloudprovider.NewRegistry()
	cp := cpfake.New()
	if err := registry.Register("FakeProviderClass", cp); err != nil {
		t.Fatal(err)
	}
	l := &lifecycle.Launcher{KubeClient: cl, CloudProvider: registry}
	res, err := l.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !res.Requeue {
		t.Errorf("want Requeue, got %+v", res)
	}
	if claim.Status.ProviderID == "" {
		t.Error("ProviderID not hydrated")
	}
	if claim.Status.PublicIP == "" {
		t.Error("PublicIP not hydrated")
	}
	found := false
	for _, c := range claim.Status.Conditions {
		if c.Type == v1alpha1.ConditionTypeLaunched && c.Status == metav1.ConditionTrue {
			found = true
		}
	}
	if !found {
		t.Errorf("Launched=True missing: %+v", claim.Status.Conditions)
	}
}

func TestLauncher_ProviderError(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("c2")
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	registry := cloudprovider.NewRegistry()
	cp := cpfake.New()
	cp.CreateFailure = errors.New("boom")
	if err := registry.Register("FakeProviderClass", cp); err != nil {
		t.Fatal(err)
	}
	l := &lifecycle.Launcher{KubeClient: cl, CloudProvider: registry}
	res, err := l.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("want RequeueAfter, got %+v", res)
	}
	c := findCond(claim, v1alpha1.ConditionTypeLaunched)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != v1alpha1.ReasonProviderError {
		t.Errorf("expected Launched=False/ProviderError, got %+v", c)
	}
}

func TestLauncher_AlreadyLaunched(t *testing.T) {
	mustAddSchemes(t)
	claim := newClaim("c3")
	claim.Status.Conditions = []metav1.Condition{{
		Type: v1alpha1.ConditionTypeLaunched, Status: metav1.ConditionTrue,
		Reason: v1alpha1.ReasonProvisioned, LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(claim).WithStatusSubresource(claim).Build()
	registry := cloudprovider.NewRegistry()
	cp := cpfake.New()
	if err := registry.Register("FakeProviderClass", cp); err != nil {
		t.Fatal(err)
	}
	l := &lifecycle.Launcher{KubeClient: cl, CloudProvider: registry}
	res, err := l.Reconcile(context.Background(), claim)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("expected no-op result, got %+v", res)
	}
	exits, _ := cp.List(context.Background())
	if len(exits) != 0 {
		t.Errorf("expected no Create call; got %d exits", len(exits))
	}
}

func findCond(claim *v1alpha1.ExitClaim, t string) *metav1.Condition {
	for i := range claim.Status.Conditions {
		if claim.Status.Conditions[i].Type == t {
			return &claim.Status.Conditions[i]
		}
	}
	return nil
}
