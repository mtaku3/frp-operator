package controller

import (
	"context"
	"strconv"
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newSchemeForPorts(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func TestReservePortsOnEmpty(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec:       frpv1alpha1.ExitServerSpec{Provider: frpv1alpha1.ProviderDigitalOcean, Frps: frpv1alpha1.FrpsConfig{Version: "v0.68.1"}, AllowPorts: []string{"1024-65535"}},
		Status:     frpv1alpha1.ExitServerStatus{Allocations: map[string]string{}},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	tunnelKey := "ns/foo"
	if err := reservePorts(ctx, cli, exit, []int32{443, 80}, tunnelKey); err != nil {
		t.Fatalf("reservePorts: %v", err)
	}
	if exit.Status.Allocations["443"] != tunnelKey {
		t.Errorf("port 443 not allocated to %q: %v", tunnelKey, exit.Status.Allocations)
	}
	if exit.Status.Allocations["80"] != tunnelKey {
		t.Errorf("port 80 not allocated to %q: %v", tunnelKey, exit.Status.Allocations)
	}
	if exit.Status.Usage.Tunnels != 1 {
		t.Errorf("Usage.Tunnels = %d, want 1", exit.Status.Usage.Tunnels)
	}
}

func TestReservePortsConflict(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec:       frpv1alpha1.ExitServerSpec{Provider: frpv1alpha1.ProviderDigitalOcean, Frps: frpv1alpha1.FrpsConfig{Version: "v0.68.1"}, AllowPorts: []string{"1024-65535"}},
		Status: frpv1alpha1.ExitServerStatus{
			Allocations: map[string]string{
				strconv.Itoa(443): "ns/other",
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	err := reservePorts(ctx, cli, exit, []int32{443}, "ns/me")
	if err == nil {
		t.Fatal("expected port-conflict error")
	}
}

func TestReleasePorts(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Status: frpv1alpha1.ExitServerStatus{
			Allocations: map[string]string{"80": "ns/foo", "443": "ns/foo", "5432": "ns/other"},
			Usage:       frpv1alpha1.ExitUsage{Tunnels: 2},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	if err := releasePorts(ctx, cli, exit, "ns/foo"); err != nil {
		t.Fatalf("releasePorts: %v", err)
	}
	if _, ok := exit.Status.Allocations["80"]; ok {
		t.Errorf("port 80 should be released")
	}
	if _, ok := exit.Status.Allocations["443"]; ok {
		t.Errorf("port 443 should be released")
	}
	if exit.Status.Allocations["5432"] != "ns/other" {
		t.Error("other tunnel's allocation accidentally removed")
	}
	if exit.Status.Usage.Tunnels != 1 {
		t.Errorf("Usage.Tunnels = %d, want 1", exit.Status.Usage.Tunnels)
	}
}
