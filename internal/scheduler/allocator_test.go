package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// allocatorTestExits builds a deterministic three-exit fleet for ranking
// tests. exit-empty has no allocations; exit-half has 5 allocations and 1
// tunnel; exit-full has 10 allocations and 2 tunnels.
func allocatorTestExits() []frpv1alpha1.ExitServer {
	mk := func(name string, allocs int, tunnels int32) frpv1alpha1.ExitServer {
		a := map[string]string{}
		for i := range allocs {
			a[itoa(20000+i)] = "ns/x"
		}
		return frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:   frpv1alpha1.ProviderDigitalOcean,
				Region:     "nyc1",
				AllowPorts: []string{"80", "1024-65535"},
			},
			Status: frpv1alpha1.ExitServerStatus{
				Phase:       frpv1alpha1.PhaseReady,
				Allocations: a,
				Usage:       frpv1alpha1.ExitUsage{Tunnels: tunnels},
			},
		}
	}
	return []frpv1alpha1.ExitServer{
		mk("exit-empty", 0, 0),
		mk("exit-half", 5, 1),
		mk("exit-full", 10, 2),
	}
}

func itoa(n int) string {
	return string([]byte{byte('0' + (n/10000)%10), byte('0' + (n/1000)%10), byte('0' + (n/100)%10), byte('0' + (n/10)%10), byte('0' + n%10)})
}

func basicTunnel() *frpv1alpha1.Tunnel {
	return &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "t", Namespace: "default"},
		Spec: frpv1alpha1.TunnelSpec{
			Service: frpv1alpha1.ServiceRef{Name: "s", Namespace: "default"},
			Ports:   []frpv1alpha1.TunnelPort{{Name: "p", ServicePort: 80}},
		},
	}
}

func TestAllocators(t *testing.T) {
	exits := allocatorTestExits()
	tunnel := basicTunnel()

	cases := []struct {
		name      string
		allocator Allocator
		wantExit  string // empty → expect Exit==nil
	}{
		{"BinPack picks fullest eligible exit", &BinPackAllocator{}, "exit-full"},
		{"Spread picks emptiest eligible exit", &SpreadAllocator{}, "exit-empty"},
		{"CapacityAware default = BinPack ranking", &CapacityAwareAllocator{}, "exit-full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tc.allocator.Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if d.Exit == nil {
				t.Fatalf("got nil exit; reason=%q", d.Reason)
			}
			if d.Exit.Name != tc.wantExit {
				t.Errorf("got %q want %q (reason=%q)", d.Exit.Name, tc.wantExit, d.Reason)
			}
		})
	}
}

func TestAllocators_NoEligibleExitReturnsReason(t *testing.T) {
	tunnel := basicTunnel()
	// All exits in PhaseProvisioning → none eligible.
	exits := allocatorTestExits()
	for i := range exits {
		exits[i].Status.Phase = frpv1alpha1.PhaseProvisioning
	}
	cases := []Allocator{&BinPackAllocator{}, &SpreadAllocator{}, &CapacityAwareAllocator{}}
	for _, a := range cases {
		t.Run(a.Name(), func(t *testing.T) {
			d, err := a.Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if d.Exit != nil {
				t.Errorf("expected Exit==nil, got %v", d.Exit.Name)
			}
			if d.Reason == "" {
				t.Error("expected non-empty Reason")
			}
		})
	}
}

func TestAllocators_PortConflictExcludesExit(t *testing.T) {
	// Block port 80 on exit-full. BinPack should fall back to exit-half.
	exits := allocatorTestExits()
	exits[2].Status.Allocations["80"] = "ns/blocker"

	tunnel := basicTunnel()
	d, err := (&BinPackAllocator{}).Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if d.Exit == nil || d.Exit.Name != "exit-half" {
		var got string
		if d.Exit != nil {
			got = d.Exit.Name
		}
		t.Errorf("got %q want exit-half (reason=%q)", got, d.Reason)
	}
}

func TestAllocators_NamesAreStable(t *testing.T) {
	if (&BinPackAllocator{}).Name() != "BinPack" {
		t.Error()
	}
	if (&SpreadAllocator{}).Name() != "Spread" {
		t.Error()
	}
	if (&CapacityAwareAllocator{}).Name() != "CapacityAware" {
		t.Error()
	}
}
