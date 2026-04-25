package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func newFrpcScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func basicTunnelForFrpc() *frpv1alpha1.Tunnel {
	port := int32(80)
	return &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "ns"},
		Spec: frpv1alpha1.TunnelSpec{
			Service: frpv1alpha1.ServiceRef{Name: "my-svc", Namespace: "ns"},
			Ports: []frpv1alpha1.TunnelPort{
				{Name: "http", ServicePort: port, PublicPort: &port, Protocol: frpv1alpha1.ProtocolTCP},
			},
		},
	}
}

func TestEnsureFrpcSecretCreatesAndIsIdempotent(t *testing.T) {
	scheme := newFrpcScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	tunnel := basicTunnelForFrpc()

	body, err := ensureFrpcSecret(ctx, cli, tunnel, "203.0.113.1", 7000, "tok-abc", []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80, PublicPort: ptrI32(80), Protocol: frpv1alpha1.ProtocolTCP}})
	if err != nil {
		t.Fatalf("ensureFrpcSecret: %v", err)
	}
	if !strings.Contains(string(body), `serverAddr = "203.0.113.1"`) {
		t.Errorf("rendered config missing serverAddr:\n%s", body)
	}

	// Idempotency: same call returns the same body bytes (the rendering is
	// deterministic) and the Secret already exists.
	body2, err := ensureFrpcSecret(ctx, cli, tunnel, "203.0.113.1", 7000, "tok-abc", []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80, PublicPort: ptrI32(80), Protocol: frpv1alpha1.ProtocolTCP}})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(body) != string(body2) {
		t.Error("rendered body changed across calls")
	}

	var sec corev1.Secret
	if err := cli.Get(ctx, types.NamespacedName{Name: "t1-frpc-config", Namespace: "ns"}, &sec); err != nil {
		t.Fatalf("Secret should exist: %v", err)
	}
	if string(sec.Data["frpc.toml"]) != string(body) {
		t.Errorf("Secret data mismatch")
	}
}

func TestEnsureFrpcDeploymentCreatesWithExpectedSpec(t *testing.T) {
	scheme := newFrpcScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	tunnel := basicTunnelForFrpc()

	if err := ensureFrpcDeployment(ctx, cli, tunnel); err != nil {
		t.Fatalf("ensureFrpcDeployment: %v", err)
	}
	var dep appsv1.Deployment
	if err := cli.Get(ctx, types.NamespacedName{Name: "t1-frpc", Namespace: "ns"}, &dep); err != nil {
		t.Fatalf("Deployment should exist: %v", err)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica, got %v", dep.Spec.Replicas)
	}
	pod := dep.Spec.Template.Spec
	if len(pod.Containers) != 1 || pod.Containers[0].Name != "frpc" {
		t.Errorf("expected single 'frpc' container, got %+v", pod.Containers)
	}
	mounts := pod.Containers[0].VolumeMounts
	if len(mounts) == 0 || mounts[0].MountPath != "/etc/frp" {
		t.Errorf("expected /etc/frp mount, got %+v", mounts)
	}
}

func ptrI32(v int32) *int32 { return &v }
