package scheduling

import (
	"testing"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func tp(public *int32) v1alpha1.TunnelPort {
	return v1alpha1.TunnelPort{PublicPort: public, ServicePort: 8080, Protocol: "TCP"}
}

func i32p(v int32) *int32 { return &v }

func TestPortsFit_SpecificHit(t *testing.T) {
	if !PortsFit([]string{"80", "443"}, nil, nil, []v1alpha1.TunnelPort{tp(i32p(80))}) {
		t.Fatal("80 should fit")
	}
}

func TestPortsFit_SpecificMiss(t *testing.T) {
	if PortsFit([]string{"80"}, nil, nil, []v1alpha1.TunnelPort{tp(i32p(443))}) {
		t.Fatal("443 not in allow")
	}
}

func TestPortsFit_RangeExpansion(t *testing.T) {
	if !PortsFit([]string{"1024-1026"}, nil, nil, []v1alpha1.TunnelPort{tp(i32p(1025))}) {
		t.Fatal("1025 in range")
	}
}

func TestResolveAutoAssign_LowestFree(t *testing.T) {
	out, ok := ResolveAutoAssign([]string{"1000-1002"}, []int32{1001}, nil, []v1alpha1.TunnelPort{tp(nil)})
	if !ok {
		t.Fatal("auto-assign should succeed")
	}
	if len(out) != 1 || out[0] != 1000 {
		t.Fatalf("expected 1000 lowest, got %v", out)
	}
}

func TestResolveAutoAssign_ExceedsAvailability(t *testing.T) {
	if _, ok := ResolveAutoAssign([]string{"1000"}, nil, nil, []v1alpha1.TunnelPort{tp(nil), tp(nil)}); ok {
		t.Fatal("should fail when 2 autos and only 1 free")
	}
}

func TestResolveAutoAssign_MixedExplicitAndAuto(t *testing.T) {
	out, ok := ResolveAutoAssign([]string{"80", "443", "8080"}, nil, nil,
		[]v1alpha1.TunnelPort{tp(i32p(443)), tp(nil)})
	if !ok {
		t.Fatal("mixed should fit")
	}
	if out[0] != 443 {
		t.Fatalf("expected explicit 443, got %d", out[0])
	}
	if out[1] != 80 {
		t.Fatalf("expected auto-lowest 80, got %d", out[1])
	}
}

func TestPortsFit_UsedExcluded(t *testing.T) {
	used := map[int32]struct{}{80: {}}
	if PortsFit([]string{"80"}, nil, used, []v1alpha1.TunnelPort{tp(i32p(80))}) {
		t.Fatal("used port should not fit")
	}
}
