package scheduler

import (
	"sort"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// BinPackAllocator prefers the densest eligible exit (most allocations +
// most tunnels). Ties broken by name for deterministic output.
type BinPackAllocator struct{}

// Name implements Allocator.
func (BinPackAllocator) Name() string { return "BinPack" }

// Allocate implements Allocator.
func (BinPackAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	eligible := EligibleExits(in.Exits, in.Tunnel)
	if len(eligible) == 0 {
		return AllocationDecision{Reason: "no eligible exit (port conflict, capacity, placement, or none Ready)"}, nil
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		di := density(eligible[i])
		dj := density(eligible[j])
		if di != dj {
			return di > dj
		}
		return eligible[i].Name < eligible[j].Name
	})
	chosen := eligible[0]
	return AllocationDecision{Exit: &chosen}, nil
}

// density is the BinPack ranking score: a higher score means a more loaded
// exit. Tunnels and allocations are weighted equally — both signal load.
func density(e frpv1alpha1.ExitServer) int {
	return len(e.Status.Allocations) + int(e.Status.Usage.Tunnels)
}
