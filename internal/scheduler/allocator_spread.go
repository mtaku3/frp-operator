package scheduler

import "sort"

// SpreadAllocator prefers the least-loaded eligible exit (fewest allocations
// + tunnels). Use when fault isolation matters more than minimizing exit
// count. Ties broken by name.
type SpreadAllocator struct{}

// Name implements Allocator.
func (SpreadAllocator) Name() string { return "Spread" }

// Allocate implements Allocator.
func (SpreadAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	eligible := EligibleExits(in.Exits, in.Tunnel)
	if len(eligible) == 0 {
		return AllocationDecision{Reason: "no eligible exit (port conflict, capacity, placement, or none Ready)"}, nil
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		di := density(eligible[i])
		dj := density(eligible[j])
		if di != dj {
			return di < dj
		}
		return eligible[i].Name < eligible[j].Name
	})
	chosen := eligible[0]
	return AllocationDecision{Exit: &chosen}, nil
}
