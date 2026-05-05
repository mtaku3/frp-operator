package methods

import (
	"context"
	"time"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// Expiration disrupts exits whose age exceeds the claim's ExpireAfter. It is
// Forceful — bypasses budgets — and emits one replacement per candidate so
// the pool capacity is preserved across the cycle.
type Expiration struct {
	Now func() time.Time
}

func NewExpiration() *Expiration { return &Expiration{Now: time.Now} }

func (m *Expiration) Name() string                      { return "Expiration" }
func (m *Expiration) Reason() v1alpha1.DisruptionReason { return v1alpha1.DisruptionReasonExpired }
func (m *Expiration) Forceful() bool                    { return true }

func (m *Expiration) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
	if c == nil || c.Claim == nil {
		return false
	}
	if c.State != nil && c.State.IsMarkedForDeletion() {
		return false
	}
	expireAfter := c.Claim.Spec.ExpireAfter.Duration
	if expireAfter <= 0 {
		return false
	}
	created := c.Claim.CreationTimestamp.Time
	if created.IsZero() {
		return false
	}
	return m.now().Sub(created) >= expireAfter
}

func (m *Expiration) ComputeCommands(
	_ context.Context,
	budgets disruption.BudgetMap,
	candidates ...*disruption.Candidate,
) ([]*disruption.Command, error) {
	// Forceful: the controller injects MaxInt32 for every relevant pool into
	// `budgets`, so we route through the shared helper as-is. The controller
	// is the single source of truth for the bypass.
	return computePerPoolWithReplacements(m.Name(), v1alpha1.DisruptionReasonExpired, budgets, candidates)
}

func (m *Expiration) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
