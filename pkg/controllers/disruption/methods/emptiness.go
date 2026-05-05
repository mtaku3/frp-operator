package methods

import (
	"context"
	"time"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// DefaultConsolidateAfter is used when the pool's Disruption.ConsolidateAfter
// is unset. Picked to match the value documented in the spec.
const DefaultConsolidateAfter = 5 * time.Minute

// Emptiness reclaims exits that have been empty (no bound tunnels) for at
// least Pool.Spec.Disruption.ConsolidateAfter.
type Emptiness struct {
	Now func() time.Time
}

// NewEmptiness returns a default-configured Emptiness method.
func NewEmptiness() *Emptiness {
	return &Emptiness{Now: time.Now}
}

func (m *Emptiness) Name() string                      { return "Emptiness" }
func (m *Emptiness) Reason() v1alpha1.DisruptionReason { return v1alpha1.DisruptionReasonEmpty }
func (m *Emptiness) Forceful() bool                    { return false }

func (m *Emptiness) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
	if c == nil || c.State == nil || c.Claim == nil || c.Pool == nil {
		return false
	}
	if !c.State.IsEmpty() {
		return false
	}
	if c.State.IsMarkedForDeletion() {
		return false
	}
	consolidateAfter := c.Pool.Spec.Disruption.ConsolidateAfter.Duration
	if consolidateAfter == 0 {
		consolidateAfter = DefaultConsolidateAfter
	}
	now := m.now()
	return !c.LastBindingChange.IsZero() && now.Sub(c.LastBindingChange) >= consolidateAfter
}

func (m *Emptiness) ComputeCommands(
	_ context.Context,
	budgets disruption.BudgetMap,
	candidates ...*disruption.Candidate,
) ([]*disruption.Command, error) {
	perPool := map[string][]*disruption.Candidate{}
	for _, c := range candidates {
		if c == nil || c.Pool == nil {
			continue
		}
		perPool[c.Pool.Name] = append(perPool[c.Pool.Name], c)
	}
	out := make([]*disruption.Command, 0, len(perPool))
	for poolName, cs := range perPool {
		allowed := budgets.Allowed(poolName, v1alpha1.DisruptionReasonEmpty)
		if allowed <= 0 {
			continue
		}
		if len(cs) > allowed {
			cs = cs[:allowed]
		}
		out = append(out, &disruption.Command{
			Candidates: cs,
			Reason:     v1alpha1.DisruptionReasonEmpty,
			Method:     m.Name(),
		})
	}
	return out, nil
}

func (m *Emptiness) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
