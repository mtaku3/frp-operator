package hash_test

import (
	"testing"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/hash"
)

func newPool() *v1alpha1.ExitPool {
	return &v1alpha1.ExitPool{
		Spec: v1alpha1.ExitPoolSpec{
			Template: v1alpha1.ExitClaimTemplate{
				Spec: v1alpha1.ExitClaimTemplateSpec{
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
			},
		},
	}
}

func TestPoolTemplateHash_Deterministic(t *testing.T) {
	p := newPool()
	h1, err := hash.PoolTemplateHash(p)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	h2, err := hash.PoolTemplateHash(p)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("non-deterministic: %s vs %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("expected 16-char hash, got %d (%q)", len(h1), h1)
	}
}

func TestPoolTemplateHash_VersionChange(t *testing.T) {
	a := newPool()
	b := newPool()
	b.Spec.Template.Spec.Frps.Version = "v0.69.0"
	ha, _ := hash.PoolTemplateHash(a)
	hb, _ := hash.PoolTemplateHash(b)
	if ha == hb {
		t.Errorf("expected different hashes for differing Frps.Version, got %s == %s", ha, hb)
	}
}

func TestPoolTemplateHash_RequirementOrderInsensitive(t *testing.T) {
	a := newPool()
	a.Spec.Template.Spec.Requirements = []v1alpha1.NodeSelectorRequirementWithMinValues{
		{Key: "topology.frp.io/region", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"sgp1", "nyc3"}},
		{Key: "frp.operator.io/tier", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"premium"}},
	}
	b := newPool()
	b.Spec.Template.Spec.Requirements = []v1alpha1.NodeSelectorRequirementWithMinValues{
		{Key: "frp.operator.io/tier", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"premium"}},
		{Key: "topology.frp.io/region", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"nyc3", "sgp1"}},
	}
	ha, _ := hash.PoolTemplateHash(a)
	hb, _ := hash.PoolTemplateHash(b)
	if ha != hb {
		t.Errorf("requirement-order-insensitive contract violated: %s != %s", ha, hb)
	}
}

func TestPoolTemplateHash_AllowPortsOrderInsensitive(t *testing.T) {
	a := newPool()
	a.Spec.Template.Spec.Frps.AllowPorts = []string{"80", "443", "1024-65535"}
	b := newPool()
	b.Spec.Template.Spec.Frps.AllowPorts = []string{"1024-65535", "443", "80"}
	ha, _ := hash.PoolTemplateHash(a)
	hb, _ := hash.PoolTemplateHash(b)
	if ha != hb {
		t.Errorf("allowPorts-order-insensitive contract violated: %s != %s", ha, hb)
	}
}
