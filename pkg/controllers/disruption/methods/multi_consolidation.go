package methods

import (
	"context"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// MultiNodeConsolidation v1: tries to drain pairs of exits at once. For a pool
// with N eligible candidates we attempt the (i, i+1) pairs greedily; if both
// can be re-packed onto the rest of the cluster, emit one command with both
// targets.
//
// Karpenter's full implementation does a binary-search over arbitrary subsets;
// pair-only consolidation is a documented v1 limitation that we accept.
type MultiNodeConsolidation struct {
	Sim *Simulator
}

func NewMultiNodeConsolidation(s *Simulator) *MultiNodeConsolidation {
	return &MultiNodeConsolidation{Sim: s}
}

func (m *MultiNodeConsolidation) Name() string { return "MultiNodeConsolidation" }
func (m *MultiNodeConsolidation) Reason() v1alpha1.DisruptionReason {
	return v1alpha1.DisruptionReasonUnderutilized
}
func (m *MultiNodeConsolidation) Forceful() bool { return false }

func (m *MultiNodeConsolidation) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
	if c == nil || c.Pool == nil || c.State == nil {
		return false
	}
	if c.State.IsMarkedForDeletion() {
		return false
	}
	if c.Pool.Spec.Disruption.ConsolidationPolicy != v1alpha1.ConsolidationWhenEmptyOrUnderutilized {
		return false
	}
	return !c.State.IsEmpty()
}

func (m *MultiNodeConsolidation) ComputeCommands(
	ctx context.Context,
	budgets disruption.BudgetMap,
	candidates ...*disruption.Candidate,
) ([]*disruption.Command, error) {
	if m.Sim == nil {
		return nil, nil
	}
	// Bucket per pool; budget is per-pool.
	perPool := map[string][]*disruption.Candidate{}
	for _, c := range candidates {
		perPool[c.Pool.Name] = append(perPool[c.Pool.Name], c)
	}
	out := []*disruption.Command{}
	for poolName, cs := range perPool {
		if len(cs) < 2 {
			continue
		}
		i := 0
		for i+1 < len(cs) {
			if budgets.Allowed(poolName, v1alpha1.DisruptionReasonUnderutilized) < 2 {
				break
			}
			pair := []*disruption.Candidate{cs[i], cs[i+1]}
			if err := m.Sim.CanRepack(ctx, pair); err == nil {
				out = append(out, &disruption.Command{
					Candidates: pair,
					Reason:     v1alpha1.DisruptionReasonUnderutilized,
					Method:     m.Name(),
				})
				remaining := budgets.Allowed(poolName, v1alpha1.DisruptionReasonUnderutilized) - 2
				budgets.Set(poolName, v1alpha1.DisruptionReasonUnderutilized, remaining)
				i += 2
				continue
			}
			i++
		}
	}
	return out, nil
}
