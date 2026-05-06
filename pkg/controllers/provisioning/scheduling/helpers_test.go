package scheduling

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// fakeCP is a minimal CloudProvider for scheduler unit tests. Returns
// one InstanceType ("fake-1") with abundant capacity and a single $0
// offering. Lets SelectInstanceType succeed for any pool that doesn't
// pin contradictory requirements.
type fakeCP struct{}

func (fakeCP) Name() string { return "fake" }
func (fakeCP) Create(_ context.Context, c *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	return c, nil
}
func (fakeCP) Delete(_ context.Context, _ *v1alpha1.ExitClaim) error { return nil }
func (fakeCP) Get(_ context.Context, _ string) (*v1alpha1.ExitClaim, error) {
	return &v1alpha1.ExitClaim{}, nil
}
func (fakeCP) List(_ context.Context) ([]*v1alpha1.ExitClaim, error) { return nil, nil }
func (fakeCP) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return []*cloudprovider.InstanceType{{
		Name: "fake-1",
		Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
			{Key: v1alpha1.RequirementInstanceType, Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"fake-1"}},
		},
		Offerings: cloudprovider.Offerings{{Available: true, Price: 0}},
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}}, nil
}
func (fakeCP) IsDrifted(_ context.Context, _ *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	return "", nil
}
func (fakeCP) RepairPolicies() []cloudprovider.RepairPolicy { return nil }
func (fakeCP) GetSupportedProviderClasses() []client.Object { return nil }

// fakeRegistry returns a Registry with FakeProviderClass → fakeCP.
func fakeRegistry() *cloudprovider.Registry {
	r := cloudprovider.NewRegistry()
	_ = r.Register("FakeProviderClass", fakeCP{})
	return r
}

func readyClaim() *v1alpha1.ExitClaim {
	const name = "e1"
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitClaimSpec{
			Frps: v1alpha1.FrpsConfig{
				Version: "v0.68.1", Auth: v1alpha1.FrpsAuthConfig{Method: "token"},
				AllowPorts: []string{"80", "443", "1024-1030"},
			},
		},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: "fake://" + name,
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Reason: "Ok", Message: "ok"}},
		},
	}
}

func tunnelWithPorts(name string, ports ...int32) *v1alpha1.Tunnel {
	tps := make([]v1alpha1.TunnelPort, 0, len(ports))
	for _, p := range ports {
		pp := p
		tps = append(tps, v1alpha1.TunnelPort{PublicPort: &pp, ServicePort: 8080, Protocol: "TCP"})
	}
	return &v1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec:       v1alpha1.TunnelSpec{Ports: tps},
	}
}

func TestExistingExit_CanAdd_RejectsNotReady(t *testing.T) {
	claim := readyClaim()
	claim.Status.Conditions = nil
	se := &state.StateExit{Claim: claim, Allocations: map[int32]state.TunnelKey{}}
	e := &ExistingExit{State: se}
	if _, err := e.CanAdd(tunnelWithPorts("t1", 80)); err == nil {
		t.Fatal("not-ready should reject")
	}
}

func TestExistingExit_CanAdd_PortConflict(t *testing.T) {
	claim := readyClaim()
	se := &state.StateExit{Claim: claim, Allocations: map[int32]state.TunnelKey{80: "default/other"}}
	e := &ExistingExit{State: se}
	if _, err := e.CanAdd(tunnelWithPorts("t1", 80)); err == nil {
		t.Fatal("port conflict should reject")
	}
}

func TestExistingExit_CanAdd_HappyPath(t *testing.T) {
	claim := readyClaim()
	se := &state.StateExit{Claim: claim, Allocations: map[int32]state.TunnelKey{}}
	e := &ExistingExit{State: se}
	out, err := e.CanAdd(tunnelWithPorts("t1", 80))
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if len(out) != 1 || out[0] != 80 {
		t.Fatalf("expected [80], got %v", out)
	}
}

func TestExistingExit_AddBlocksSecondClaim(t *testing.T) {
	claim := readyClaim()
	se := &state.StateExit{Claim: claim, Allocations: map[int32]state.TunnelKey{}}
	e := &ExistingExit{State: se}
	t1 := tunnelWithPorts("t1", 80)
	out, err := e.CanAdd(t1)
	if err != nil {
		t.Fatalf("first ok: %v", err)
	}
	e.Add(t1, out)
	if _, err := e.CanAdd(tunnelWithPorts("t2", 80)); err == nil {
		t.Fatal("second claim on same port should fail after Add")
	}
}

func TestInflightClaim_CanAddAndPack(t *testing.T) {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
		Spec: v1alpha1.ExitPoolSpec{
			Template: v1alpha1.ExitClaimTemplate{
				Spec: v1alpha1.ExitClaimTemplateSpec{
					ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
					Frps: v1alpha1.FrpsConfig{
						Version: "v0.68.1", Auth: v1alpha1.FrpsAuthConfig{Method: "token"},
						AllowPorts: []string{"80", "443"},
					},
				},
			},
		},
	}
	c := NewClaimFromPool(pool, "tunnel-uid-A")
	t1 := tunnelWithPorts("t1", 80)
	out, err := c.CanAdd(t1)
	if err != nil {
		t.Fatalf("first ok: %v", err)
	}
	c.Add(t1, out)
	t2 := tunnelWithPorts("t2", 80)
	if _, err := c.CanAdd(t2); err == nil {
		t.Fatal("port reuse on inflight should fail")
	}
	t3 := tunnelWithPorts("t3", 443)
	if _, err := c.CanAdd(t3); err != nil {
		t.Fatalf("443 should fit: %v", err)
	}
}

func TestNewClaimFromPool_DeterministicName(t *testing.T) {
	pool := &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p1"}}
	a := NewClaimFromPool(pool, "uid-A")
	b := NewClaimFromPool(pool, "uid-A")
	c := NewClaimFromPool(pool, "uid-B")
	if a.Name != b.Name {
		t.Fatalf("same tunnel UID should produce same name; got %s vs %s", a.Name, b.Name)
	}
	if a.Name == c.Name {
		t.Fatal("different tunnel UID should produce different name")
	}
}

func TestSortByLoad(t *testing.T) {
	a := &InflightClaim{Name: "a", Tunnels: make([]*v1alpha1.Tunnel, 3)}
	b := &InflightClaim{Name: "b", Tunnels: make([]*v1alpha1.Tunnel, 1)}
	c := &InflightClaim{Name: "c", Tunnels: make([]*v1alpha1.Tunnel, 2)}
	in := []*InflightClaim{a, b, c}
	SortByLoad(in)
	if in[0].Name != "b" || in[1].Name != "c" || in[2].Name != "a" {
		t.Fatalf("expected b,c,a; got %v", []string{in[0].Name, in[1].Name, in[2].Name})
	}
}

func TestPreferences_RelaxAlwaysFalseInV1(t *testing.T) {
	p := &Preferences{Policy: "Respect"}
	if p.Relax(&v1alpha1.Tunnel{}) {
		t.Fatal("v1 Relax should always be false")
	}
}
