package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const (
	testExitName = "exit-1"
	testNS       = "default"
)

func newSchemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func TestEnsureCredentialsSecretCreatesNewSecret(t *testing.T) {
	scheme := newSchemeForTest(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	exit := &frpv1alpha1.ExitServer{}
	exit.Name = testExitName
	exit.Namespace = testNS

	got, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("ensureCredentialsSecret: %v", err)
	}
	if got.AdminPassword == "" || len(got.AdminPassword) < 32 {
		t.Errorf("AdminPassword too short: %d chars", len(got.AdminPassword))
	}
	if got.AuthToken == "" || len(got.AuthToken) < 32 {
		t.Errorf("AuthToken too short: %d chars", len(got.AuthToken))
	}

	// Secret should now exist in the cluster.
	var sec corev1.Secret
	if err := cli.Get(ctx, types.NamespacedName{Name: "exit-1-credentials", Namespace: testNS}, &sec); err != nil {
		t.Fatalf("Get secret: %v", err)
	}
	if string(sec.Data["admin-password"]) != got.AdminPassword {
		t.Error("Secret admin-password mismatch")
	}
	if string(sec.Data["auth-token"]) != got.AuthToken {
		t.Error("Secret auth-token mismatch")
	}
	if sec.Labels["frp-operator.io/exit"] != testExitName {
		t.Errorf("expected label frp-operator.io/exit=exit-1, got %q", sec.Labels["frp-operator.io/exit"])
	}
}

func TestEnsureCredentialsSecretIsIdempotent(t *testing.T) {
	scheme := newSchemeForTest(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	exit := &frpv1alpha1.ExitServer{}
	exit.Name = testExitName
	exit.Namespace = testNS

	first, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.AdminPassword != second.AdminPassword {
		t.Error("AdminPassword changed across calls")
	}
	if first.AuthToken != second.AuthToken {
		t.Error("AuthToken changed across calls")
	}
}
