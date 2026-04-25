package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func basicPolicy(maxExits *int32) *frpv1alpha1.SchedulingPolicy {
	return &frpv1alpha1.SchedulingPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: frpv1alpha1.SchedulingPolicySpec{
			Budget: frpv1alpha1.BudgetSpec{MaxExits: maxExits},
			VPS: frpv1alpha1.VPSSpec{
				Default: frpv1alpha1.VPSDefaults{
					Provider: frpv1alpha1.ProviderDigitalOcean,
					Regions:  []string{"nyc1", "sfo3"},
					Size:     "s-1vcpu-1gb",
				},
			},
		},
	}
}

func TestOnDemand_ProvisionsWhenUnderBudget(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(3)
	d, err := p.Plan(ProvisionInput{
		Tunnel:  basicTunnel(80),
		Policy:  basicPolicy(&max),
		Current: nil,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !d.Provision {
		t.Errorf("expected Provision=true; reason=%q", d.Reason)
	}
	if d.Spec.Provider != frpv1alpha1.ProviderDigitalOcean {
		t.Errorf("Spec.Provider=%q want digitalocean", d.Spec.Provider)
	}
	if d.Spec.Region != "nyc1" {
		t.Errorf("Spec.Region=%q want nyc1 (first in policy regions)", d.Spec.Region)
	}
	if d.Spec.Size != "s-1vcpu-1gb" {
		t.Errorf("Spec.Size=%q want s-1vcpu-1gb", d.Spec.Size)
	}
}

func TestOnDemand_RefusesWhenAtBudget(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(2)
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: current})
	if d.Provision {
		t.Errorf("expected Provision=false; got Spec=%+v", d.Spec)
	}
	if d.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestOnDemand_RefusesWhenNamespaceCapped(t *testing.T) {
	p := &OnDemandStrategy{}
	maxNs := int32(1)
	policy := basicPolicy(nil)
	policy.Spec.Budget.MaxExitsPerNamespace = &maxNs
	t1 := basicTunnel(80)
	t1.Namespace = "team-a"
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "team-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "team-b"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: t1, Policy: policy, Current: current})
	if d.Provision {
		t.Errorf("expected Provision=false (per-ns budget hit); got Spec=%+v", d.Spec)
	}
}

func TestOnDemand_AppliesPlacementOverrides(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(5)
	tunnel := basicTunnel(80)
	tunnel.Spec.Placement = &frpv1alpha1.Placement{
		Regions:      []string{"sfo3"},
		SizeOverride: "s-2vcpu-4gb",
	}
	d, err := p.Plan(ProvisionInput{Tunnel: tunnel, Policy: basicPolicy(&max), Current: nil})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !d.Provision {
		t.Fatalf("Provision=false; reason=%q", d.Reason)
	}
	if d.Spec.Region != "sfo3" {
		t.Errorf("Region=%q want sfo3", d.Spec.Region)
	}
	if d.Spec.Size != "s-2vcpu-4gb" {
		t.Errorf("Size=%q want s-2vcpu-4gb", d.Spec.Size)
	}
}

func TestFixedPool_RefusesBeyondPool(t *testing.T) {
	p := &FixedPoolStrategy{}
	max := int32(2)
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: current})
	if d.Provision {
		t.Error("FixedPool must refuse beyond MaxExits")
	}
}

func TestFixedPool_ProvisionsBelowPool(t *testing.T) {
	p := &FixedPoolStrategy{}
	max := int32(3)
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: nil})
	if !d.Provision {
		t.Error("FixedPool must provision below MaxExits")
	}
}

func TestProvisionStrategyNames(t *testing.T) {
	if (&OnDemandStrategy{}).Name() != "OnDemand" {
		t.Error()
	}
	if (&FixedPoolStrategy{}).Name() != "FixedPool" {
		t.Error()
	}
}
