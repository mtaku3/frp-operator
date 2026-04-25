package v1alpha1

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func mkTunnel(opts ...func(*frpv1alpha1.Tunnel)) *frpv1alpha1.Tunnel {
	t := &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "t", Namespace: "ns"},
		Spec: frpv1alpha1.TunnelSpec{
			Service: frpv1alpha1.ServiceRef{Name: "svc", Namespace: "ns"},
			Ports:   []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80}},
		},
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

func TestTunnelValidator_AllowsCreateAndDelete(t *testing.T) {
	v := &TunnelValidator{}
	if _, err := v.ValidateCreate(context.Background(), mkTunnel()); err != nil {
		t.Errorf("ValidateCreate: %v", err)
	}
	if _, err := v.ValidateDelete(context.Background(), mkTunnel()); err != nil {
		t.Errorf("ValidateDelete: %v", err)
	}
}

func TestTunnelValidator_AllowsUpdatesWhenNotLocked(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel()
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		// Change something; ImmutableWhenReady is false.
		t.Spec.MigrationPolicy = frpv1alpha1.MigrationOnExitLost
	})
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("ValidateUpdate: %v", err)
	}
}

func TestTunnelValidator_AllowsUpdatesWhenLockedButNotReady(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelConnecting
	})
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelConnecting
		t.Spec.ExitRef = &frpv1alpha1.ExitRef{Name: "new-exit"}
	})
	// Locked, but not Ready -> changes still allowed.
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("ValidateUpdate: %v", err)
	}
}

func TestTunnelValidator_RejectsExitRefChangeWhenLockedAndReady(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
		t.Spec.ExitRef = &frpv1alpha1.ExitRef{Name: "old"}
	})
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
		t.Spec.ExitRef = &frpv1alpha1.ExitRef{Name: "new"}
	})
	_, err := v.ValidateUpdate(context.Background(), old, new)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exitRef") {
		t.Errorf("error should mention exitRef: %v", err)
	}
}

func TestTunnelValidator_RejectsPortsChangeWhenLockedAndReady(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
	})
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
		t.Spec.Ports = []frpv1alpha1.TunnelPort{
			{Name: "http", ServicePort: 80},
			{Name: "https", ServicePort: 443},
		}
	})
	_, err := v.ValidateUpdate(context.Background(), old, new)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ports") {
		t.Errorf("error should mention ports: %v", err)
	}
}

func TestTunnelValidator_AllowsPlacementChangeWhenLockedAndReady(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
	})
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
		t.Spec.Placement = &frpv1alpha1.Placement{Regions: []string{"sfo3"}}
	})
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("Placement edit should be allowed when locked: %v", err)
	}
}

func TestTunnelValidator_AllowsUnlockingWhenLockedAndReady(t *testing.T) {
	v := &TunnelValidator{}
	old := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = true
		t.Status.Phase = frpv1alpha1.TunnelReady
	})
	new := mkTunnel(func(t *frpv1alpha1.Tunnel) {
		t.Spec.ImmutableWhenReady = false // explicit unlock
		t.Status.Phase = frpv1alpha1.TunnelReady
	})
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("unlocking the lock should be allowed: %v", err)
	}
}
