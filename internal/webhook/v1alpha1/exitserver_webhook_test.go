package v1alpha1

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func mkExit(opts ...func(*frpv1alpha1.ExitServer)) *frpv1alpha1.ExitServer {
	e := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec: frpv1alpha1.ExitServerSpec{
			Provider:   frpv1alpha1.ProviderDigitalOcean,
			Frps:       frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
			AllowPorts: []string{"1024-65535"},
		},
		Status: frpv1alpha1.ExitServerStatus{
			Allocations: map[string]string{},
		},
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func TestExitServerValidator_AllowsCreate(t *testing.T) {
	v := &ExitServerValidator{}
	if _, err := v.ValidateCreate(context.Background(), mkExit()); err != nil {
		t.Errorf("ValidateCreate: %v", err)
	}
}

func TestExitServerValidator_AllowsAllowPortsExpansion(t *testing.T) {
	v := &ExitServerValidator{}
	old := mkExit()
	new := mkExit(func(e *frpv1alpha1.ExitServer) {
		e.Spec.AllowPorts = []string{"80", "443", "1024-65535"}
	})
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("expansion should be allowed: %v", err)
	}
}

func TestExitServerValidator_RejectsAllowPortsShrinkBelowAllocations(t *testing.T) {
	v := &ExitServerValidator{}
	old := mkExit(func(e *frpv1alpha1.ExitServer) {
		e.Status.Allocations = map[string]string{"5432": "ns/pg"}
	})
	new := mkExit(func(e *frpv1alpha1.ExitServer) {
		e.Status.Allocations = map[string]string{"5432": "ns/pg"}
		e.Spec.AllowPorts = []string{"6000-65535"} // 5432 no longer covered
	})
	_, err := v.ValidateUpdate(context.Background(), old, new)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "5432") {
		t.Errorf("error should mention port 5432: %v", err)
	}
}

func TestExitServerValidator_AllowsShrinkWhenNoConflict(t *testing.T) {
	v := &ExitServerValidator{}
	old := mkExit(func(e *frpv1alpha1.ExitServer) {
		e.Status.Allocations = map[string]string{"443": "ns/pg"}
	})
	new := mkExit(func(e *frpv1alpha1.ExitServer) {
		e.Status.Allocations = map[string]string{"443": "ns/pg"}
		e.Spec.AllowPorts = []string{"443"} // shrunk, but 443 still covered
	})
	if _, err := v.ValidateUpdate(context.Background(), old, new); err != nil {
		t.Errorf("shrink that covers allocations should be allowed: %v", err)
	}
}
