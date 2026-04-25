package scheduler

// CapacityAwareAllocator is the default. Eligibility filtering already
// rejects over-capacity exits (see EligibleExits in eligibility.go); this
// allocator's only job is to pick among the eligible by BinPack ranking
// (densest first), giving us "fill-up-cheap-exits-first" without ever
// over-committing.
type CapacityAwareAllocator struct{}

// Name implements Allocator.
func (CapacityAwareAllocator) Name() string { return "CapacityAware" }

// Allocate implements Allocator. Delegates to BinPack ranking after the
// shared eligibility filter has done its work.
func (CapacityAwareAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	return BinPackAllocator{}.Allocate(in)
}
