package methods

import (
	"context"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// SingleNodeConsolidation tries to delete one exit at a time by re-packing
// its bound tunnels onto other ready exits.
//
// Gate: the candidate's pool must use ConsolidationPolicy =
// WhenEmptyOrUnderutilized (the WhenEmpty policy implies "only Empty
// reclaims," so this method short-circuits there).
type SingleNodeConsolidation struct {
	Sim *Simulator
}

func NewSingleNodeConsolidation(s *Simulator) *SingleNodeConsolidation {
	return &SingleNodeConsolidation{Sim: s}
}

func (m *SingleNodeConsolidation) Name() string                          { return "SingleNodeConsolidation" }
func (m *SingleNodeConsolidation) Reason() v1alpha1.DisruptionReason     { return v1alpha1.DisruptionReasonUnderutilized }
func (m *SingleNodeConsolidation) Forceful() bool                        { return false }

func (m *SingleNodeConsolidation) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
	if c == nil || c.Pool == nil || c.State == nil {
		return false
	}
	if c.State.IsMarkedForDeletion() {
		return false
	}
	if c.Pool.Spec.Disruption.ConsolidationPolicy != v1alpha1.ConsolidationWhenEmptyOrUnderutilized {
		return false
	}
	// Only consider exits that have at least one bound tunnel — empty exits
	// belong to the Emptiness method.
	return !c.State.IsEmpty()
}

func (m *SingleNodeConsolidation) ComputeCommands(ctx context.Context, budgets disruption.BudgetMap, candidates ...*disruption.Candidate) ([]*disruption.Command, error) {
	if m.Sim == nil {
		return nil, nil
	}
	out := []*disruption.Command{}
	for _, c := range candidates {
		if c == nil || c.Pool == nil {
			continue
		}
		if budgets.Allowed(c.Pool.Name, v1alpha1.DisruptionReasonUnderutilized) <= 0 {
			continue
		}
		if err := m.Sim.CanRepack(ctx, []*disruption.Candidate{c}); err != nil {
			continue
		}
		// Found one candidate we can drain. Emit a single-target command
		// with no replacements (the tunnels move onto existing exits).
		out = append(out, &disruption.Command{
			Candidates: []*disruption.Candidate{c},
			Reason:     v1alpha1.DisruptionReasonUnderutilized,
			Method:     m.Name(),
		})
		// Decrement budget for subsequent candidates in the same pool.
		budgets.Set(c.Pool.Name, v1alpha1.DisruptionReasonUnderutilized,
			budgets.Allowed(c.Pool.Name, v1alpha1.DisruptionReasonUnderutilized)-1)
	}
	return out, nil
}
