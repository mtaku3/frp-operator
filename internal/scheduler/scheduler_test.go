package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

type stubAllocator struct{ name string }

func (s *stubAllocator) Name() string { return s.name }
func (s *stubAllocator) Allocate(_ AllocateInput) (AllocationDecision, error) {
	return AllocationDecision{}, nil
}

type stubProvisionStrategy struct{ name string }

func (s *stubProvisionStrategy) Name() string { return s.name }
func (s *stubProvisionStrategy) Plan(_ ProvisionInput) (ProvisionDecision, error) {
	return ProvisionDecision{}, nil
}

func TestInterfacesShape(t *testing.T) {
	var _ Allocator = (*stubAllocator)(nil)
	var _ ProvisionStrategy = (*stubProvisionStrategy)(nil)
}

func TestAllocatorRegistry(t *testing.T) {
	r := NewAllocatorRegistry()
	if err := r.Register(&stubAllocator{name: "x"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(&stubAllocator{name: "x"}); err == nil {
		t.Fatal("expected duplicate error")
	}
	got, err := r.Lookup("x")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name() != "x" {
		t.Errorf("got %q", got.Name())
	}
	if _, err := r.Lookup("missing"); err == nil {
		t.Error("expected lookup miss to error")
	}
}

func TestAllocationDecisionZero(t *testing.T) {
	var d AllocationDecision
	if d.Exit != nil {
		t.Errorf("zero AllocationDecision.Exit must be nil")
	}
	if d.Reason != "" {
		t.Errorf("zero Reason must be empty")
	}
}

// TestProvisionDecisionWithSpec is a sanity check that the spec-carrying
// branch round-trips through reasonable use without losing information.
func TestProvisionDecisionWithSpec(t *testing.T) {
	d := ProvisionDecision{
		Provision: true,
		Spec: frpv1alpha1.ExitServerSpec{
			Provider: frpv1alpha1.ProviderDigitalOcean,
			Region:   "nyc1",
		},
	}
	if d.Spec.Provider != frpv1alpha1.ProviderDigitalOcean {
		t.Errorf("Spec.Provider lost")
	}
}
