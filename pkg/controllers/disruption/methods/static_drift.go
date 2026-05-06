package methods

import (
	"context"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// StaticDrift handles drift that doesn't require a replacement (e.g.
// label-only template changes). v1: emits no Replacements — Phase 7 is
// expected to bump the claim's pool-hash annotation in-place separately, so
// here we just delete the candidate.
//
// Until Phase 7 stamps the static-drift annotation distinct from full drift,
// this method short-circuits unconditionally.
type StaticDrift struct{}

func NewStaticDrift() *StaticDrift                       { return &StaticDrift{} }
func (m *StaticDrift) Name() string                      { return "StaticDrift" }
func (m *StaticDrift) Reason() v1alpha1.DisruptionReason { return v1alpha1.DisruptionReasonDrifted }
func (m *StaticDrift) Forceful() bool                    { return false }

func (m *StaticDrift) ShouldDisrupt(_ context.Context, _ *disruption.Candidate) bool {
	// Phase 7 will define the annotation that distinguishes static from
	// full drift. Until then this method never fires.
	return false
}

func (m *StaticDrift) ComputeCommands(
	_ context.Context,
	_ disruption.BudgetMap,
	_ ...*disruption.Candidate,
) ([]*disruption.Command, error) {
	return nil, nil
}
