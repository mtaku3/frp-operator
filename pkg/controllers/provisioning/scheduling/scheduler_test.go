package scheduling

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func newPool(name string, allowPorts []string, limits v1alpha1.Limits) *v1alpha1.ExitPool {
	w := int32(10)
	return &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitPoolSpec{
			Weight: &w,
			Limits: limits,
			Template: v1alpha1.ExitClaimTemplate{
				Spec: v1alpha1.ExitClaimTemplateSpec{
					ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
					Frps: v1alpha1.FrpsConfig{
						Version:    "v0.68.1",
						Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
						AllowPorts: allowPorts,
					},
				},
			},
		},
	}
}

func solveCtx() context.Context { return context.Background() }

func TestSolve_NoExitsNoPools_TunnelErrors(t *testing.T) {
	c := state.NewCluster(nil)
	s := New(c, cloudprovider.NewRegistry(), nil)
	res, err := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tunnelWithPorts("t1", 80)})
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if len(res.NewClaims) != 0 || len(res.Bindings) != 0 {
		t.Fatalf("expected no claims/bindings, got %+v", res)
	}
	if len(res.TunnelErrors) != 1 {
		t.Fatalf("expected 1 error, got %v", res.TunnelErrors)
	}
}

func TestSolve_OnePool_NoExits_ProducesNewClaim(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("p1", []string{"80", "443"}, nil))
	s := New(c, cloudprovider.NewRegistry(), nil)
	res, _ := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tunnelWithPorts("t1", 80)})
	if len(res.NewClaims) != 1 {
		t.Fatalf("expected 1 NewClaim, got %d", len(res.NewClaims))
	}
	if len(res.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(res.Bindings))
	}
	if res.Bindings[0].ExitClaimName != res.NewClaims[0].Name {
		t.Fatal("binding should reference the new claim")
	}
}

func TestSolve_TwoTunnels_BinpackOntoOneInflight(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("p1", []string{"80", "443"}, nil))
	s := New(c, cloudprovider.NewRegistry(), nil)
	res, _ := s.Solve(solveCtx(), []*v1alpha1.Tunnel{
		tunnelWithPorts("t1", 80),
		tunnelWithPorts("t2", 443),
	})
	if len(res.NewClaims) != 1 {
		t.Fatalf("expected 1 NewClaim (binpacked), got %d", len(res.NewClaims))
	}
	if len(res.Bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(res.Bindings))
	}
	if res.Bindings[0].ExitClaimName != res.Bindings[1].ExitClaimName {
		t.Fatal("both bindings should target the same inflight claim")
	}
}

func TestSolve_OneReadyExit_BindsExisting(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("p1", []string{"80"}, nil))
	claim := readyClaim()
	claim.Spec.Frps = v1alpha1.FrpsConfig{
		Version: "v0.68.1", Auth: v1alpha1.FrpsAuthConfig{Method: "token"},
		AllowPorts: []string{"80", "443", "1024-1030"},
	}
	c.UpdateExit(claim)
	s := New(c, cloudprovider.NewRegistry(), nil)
	res, _ := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tunnelWithPorts("t1", 80)})
	if len(res.NewClaims) != 0 {
		t.Fatalf("expected 0 NewClaims, got %d", len(res.NewClaims))
	}
	if len(res.Bindings) != 1 || res.Bindings[0].ExitClaimName != "e1" {
		t.Fatalf("expected binding to e1, got %+v", res.Bindings)
	}
}

func TestSolve_PortCollideOnExisting_ProducesNewClaim(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("p1", []string{"80", "443"}, nil))
	claim := readyClaim()
	claim.Spec.Frps.AllowPorts = []string{"80"} // only 80 available
	c.UpdateExit(claim)
	// pre-populate binding so 80 is taken
	c.UpdateTunnelBinding(state.TunnelKey("default/other"), "e1", []int32{80})

	s := New(c, cloudprovider.NewRegistry(), nil)
	res, _ := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tunnelWithPorts("t1", 80)})
	if len(res.NewClaims) != 1 {
		t.Fatalf("expected 1 NewClaim, got %d (errors=%v)", len(res.NewClaims), res.TunnelErrors)
	}
}

// TestSolve_RehydratesPendingClaim_AcrossSolves verifies cross-Solve
// idempotency: when an ExitClaim has been persisted but is not yet
// Ready, a subsequent Solve should rehydrate it as an inflight claim
// and binpack onto it rather than minting a duplicate. Regression for
// the spec-reviewer flagged hole patched alongside the stable-salt fix.
func TestSolve_RehydratesPendingClaim_AcrossSolves(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("default", []string{"80", "443"}, nil))

	// Pre-seed: an ExitClaim exists for some earlier tunnel but is not
	// Ready (just-created). Carry the exit-pool label so the rehydrate
	// pass can match it back to its pool.
	pendingClaim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "default-abc12345",
			Labels: map[string]string{v1alpha1.LabelExitPool: "default"},
		},
		Spec: v1alpha1.ExitClaimSpec{
			Frps: v1alpha1.FrpsConfig{
				Version: "v0.68.1", Auth: v1alpha1.FrpsAuthConfig{Method: "token"},
				AllowPorts: []string{"80", "443"},
			},
		},
		Status: v1alpha1.ExitClaimStatus{ProviderID: "fake://pending"}, // no Ready condition
	}
	c.UpdateExit(pendingClaim)

	// A fresh tunnel arrives in a new Solve; expected to binpack onto
	// the pending claim (no NewClaims minted).
	s := New(c, nil, nil)
	tun := tunnelWithPorts("tunnel-2", 80)
	tun.UID = "uid-2"
	res, err := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tun})
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if len(res.NewClaims) != 0 {
		t.Fatalf("expected 0 NewClaims (binpack onto pending), got %d", len(res.NewClaims))
	}
	if len(res.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(res.Bindings))
	}
	if res.Bindings[0].ExitClaimName != "default-abc12345" {
		t.Fatalf("expected binding to default-abc12345, got %q", res.Bindings[0].ExitClaimName)
	}
}

// TestSolve_StableSalt_SameNameAcrossSolves verifies that the same
// tunnel UID generates the same ExitClaim name across separate Solve
// invocations. This is the property that lets the AlreadyExists swallow
// in persistResults achieve actual idempotency on Tunnel.Status patch
// retries — without it, every Solve mints a fresh name and a duplicate
// ExitClaim slips through.
func TestSolve_StableSalt_SameNameAcrossSolves(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdatePool(newPool("default", []string{"80", "443"}, nil))

	s := New(c, nil, nil)
	tun := tunnelWithPorts("t-stable", 80)
	tun.UID = "uid-stable"

	res1, err := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tun})
	if err != nil {
		t.Fatalf("solve1: %v", err)
	}
	res2, err := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tun})
	if err != nil {
		t.Fatalf("solve2: %v", err)
	}
	if len(res1.NewClaims) != 1 || len(res2.NewClaims) != 1 {
		t.Fatalf("expected 1 NewClaim each; got %d and %d", len(res1.NewClaims), len(res2.NewClaims))
	}
	if res1.NewClaims[0].Name != res2.NewClaims[0].Name {
		t.Fatalf("expected stable claim name across Solves; got %q vs %q",
			res1.NewClaims[0].Name, res2.NewClaims[0].Name)
	}
}

func TestSolve_PoolLimitsExceeded_TunnelErrors(t *testing.T) {
	c := state.NewCluster(nil)
	pool := newPool("p1", []string{"80"}, v1alpha1.Limits{
		corev1.ResourceName(v1alpha1.ResourceExits): resource.MustParse("1"),
	})
	// Bump pool counter to 1 (>= limit) by setting Status.Resources;
	// UpdatePool mirrors that into the StatePool's running totals.
	pool.Status.Resources = corev1.ResourceList{
		corev1.ResourceName(v1alpha1.ResourceExits): resource.MustParse("1"),
	}
	pool.Status.Exits = 1
	c.UpdatePool(pool)

	s := New(c, cloudprovider.NewRegistry(), nil)
	res, _ := s.Solve(solveCtx(), []*v1alpha1.Tunnel{tunnelWithPorts("t1", 80)})
	if len(res.NewClaims) != 0 {
		t.Fatalf("limit exceeded should suppress NewClaim; got %d", len(res.NewClaims))
	}
	if len(res.TunnelErrors) != 1 {
		t.Fatalf("expected 1 tunnel error, got %v", res.TunnelErrors)
	}
}
