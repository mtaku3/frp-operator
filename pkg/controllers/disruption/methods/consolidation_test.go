package methods_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption/methods"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func newReadyClaim(name string, allocCPU string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{v1alpha1.LabelExitPool: "p"},
		},
		Spec: v1alpha1.ExitClaimSpec{
			Frps: v1alpha1.FrpsConfig{
				Version:    "v0.68.1",
				Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
				AllowPorts: []string{"80", "443", "1024-2000"},
			},
		},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: "fake://" + name,
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(allocCPU),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Conditions: []metav1.Condition{{
				Type: "Ready", Status: metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(), Reason: "Ok",
			}},
		},
	}
}

func newTunnel(name, exit string, port int32, cpu string) *v1alpha1.Tunnel {
	pp := port
	t := &v1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Spec: v1alpha1.TunnelSpec{
			Ports: []v1alpha1.TunnelPort{{PublicPort: &pp, ServicePort: 8080, Protocol: "TCP"}},
		},
		Status: v1alpha1.TunnelStatus{
			AssignedExit:  exit,
			AssignedPorts: []int32{port},
		},
	}
	if cpu != "" {
		t.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(cpu),
		}
	}
	return t
}

func setupSim(t *testing.T, claims []*v1alpha1.ExitClaim, tunnels []*v1alpha1.Tunnel) (*methods.Simulator, *state.Cluster) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	objs := []runtime.Object{}
	for _, c := range claims {
		objs = append(objs, c)
	}
	for _, tn := range tunnels {
		objs = append(objs, tn)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).WithStatusSubresource(&v1alpha1.Tunnel{}, &v1alpha1.ExitClaim{}).Build()
	cluster := state.NewCluster(kube)
	for _, c := range claims {
		cluster.UpdateExit(c)
	}
	for _, tn := range tunnels {
		cluster.UpdateTunnelBinding(state.TunnelKey(tn.Namespace+"/"+tn.Name), tn.Status.AssignedExit, tn.Status.AssignedPorts)
	}
	return methods.NewSimulator(cluster, kube), cluster
}

func TestSimulator_RepackFits(t *testing.T) {
	// Two ready exits. Candidate (e1) holds one tunnel on port 80 with no
	// CPU request. e2 has empty allocations and 4 CPU available — fits.
	e1 := newReadyClaim("e1", "4")
	e2 := newReadyClaim("e2", "4")
	tn := newTunnel("t1", "e1", 80, "")
	sim, cluster := setupSim(t, []*v1alpha1.ExitClaim{e1, e2}, []*v1alpha1.Tunnel{tn})

	cand := &disruption.Candidate{Claim: e1, State: cluster.ExitForName("e1")}
	if err := sim.CanRepack(context.Background(), []*disruption.Candidate{cand}); err != nil {
		t.Fatalf("expected repack to fit: %v", err)
	}
}

func TestSimulator_RepackPortConflict(t *testing.T) {
	// e1 has tunnel on port 80; e2 also already holds port 80 → can't move.
	e1 := newReadyClaim("e1", "4")
	e2 := newReadyClaim("e2", "4")
	t1 := newTunnel("t1", "e1", 80, "")
	t2 := newTunnel("t2", "e2", 80, "")
	sim, cluster := setupSim(t, []*v1alpha1.ExitClaim{e1, e2}, []*v1alpha1.Tunnel{t1, t2})

	cand := &disruption.Candidate{Claim: e1, State: cluster.ExitForName("e1")}
	if err := sim.CanRepack(context.Background(), []*disruption.Candidate{cand}); err == nil {
		t.Fatal("expected repack to fail on port conflict")
	}
}

func TestSimulator_NoOtherExits(t *testing.T) {
	// Only e1 exists with a bound tunnel — nowhere to move.
	e1 := newReadyClaim("e1", "4")
	t1 := newTunnel("t1", "e1", 80, "")
	sim, cluster := setupSim(t, []*v1alpha1.ExitClaim{e1}, []*v1alpha1.Tunnel{t1})

	cand := &disruption.Candidate{Claim: e1, State: cluster.ExitForName("e1")}
	if err := sim.CanRepack(context.Background(), []*disruption.Candidate{cand}); err == nil {
		t.Fatal("expected failure with no spare exits")
	}
}

func TestSingleConsolidation_PolicyGate(t *testing.T) {
	// Pool with WhenEmpty (not Underutilized): SingleConsolidation must skip.
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidationPolicy: v1alpha1.ConsolidationWhenEmpty},
		},
	}
	se := &state.StateExit{Allocations: map[int32]state.TunnelKey{80: "ns/t"}}
	cand := &disruption.Candidate{
		Claim: &v1alpha1.ExitClaim{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		State: se, Pool: pool,
	}
	m := methods.NewSingleNodeConsolidation(nil)
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("WhenEmpty policy must gate out single-node consolidation")
	}
}

func TestMultiConsolidation_NoFitWhenFull(t *testing.T) {
	// Two exits, each bound to a tunnel requesting 5 CPU. Total demand is 10
	// and any one exit only has 8 CPU — multi-consolidation must refuse.
	e1 := newReadyClaim("e1", "8")
	e2 := newReadyClaim("e2", "8")
	t1 := newTunnel("t1", "e1", 80, "5")
	t2 := newTunnel("t2", "e2", 443, "5")
	sim, cluster := setupSim(t, []*v1alpha1.ExitClaim{e1, e2}, []*v1alpha1.Tunnel{t1, t2})

	cand1 := &disruption.Candidate{Claim: e1, State: cluster.ExitForName("e1")}
	cand2 := &disruption.Candidate{Claim: e2, State: cluster.ExitForName("e2")}
	if err := sim.CanRepack(context.Background(), []*disruption.Candidate{cand1, cand2}); err == nil {
		t.Fatal("repack with no spare exits should fail")
	}
}
