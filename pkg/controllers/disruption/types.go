package disruption

import (
	"context"
	"time"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Method represents one disruption strategy. The disruption controller runs
// methods in priority order and lets the first method that produces commands
// own the loop iteration.
type Method interface {
	// Name returns the human-readable method name (used in logs/metrics).
	Name() string
	// Reason is the DisruptionReason emitted on commands produced by the method.
	Reason() v1alpha1.DisruptionReason
	// ShouldDisrupt is the per-candidate eligibility filter. It runs before
	// budget gating, so it must be cheap.
	ShouldDisrupt(ctx context.Context, c *Candidate) bool
	// ComputeCommands turns a list of eligible candidates into a list of
	// commands. Implementations are responsible for capping by budget.
	ComputeCommands(ctx context.Context, budgets BudgetMap, candidates ...*Candidate) ([]*Command, error)
	// Forceful methods bypass per-pool budgets (Expiration only in v1).
	Forceful() bool
}

// Candidate is one disruptable exit in the cluster cache.
type Candidate struct {
	Claim             *v1alpha1.ExitClaim
	State             *state.StateExit
	Pool              *v1alpha1.ExitPool
	DisruptionCost    float64
	LastBindingChange time.Time
}

// Command is one decision: which candidates to disrupt, what (if anything) to
// launch as replacements, and the reason being acted on.
type Command struct {
	Candidates   []*Candidate
	Replacements []*v1alpha1.ExitClaim
	Reason       v1alpha1.DisruptionReason
	Method       string
}

// BudgetMap is per-pool, per-reason remaining-disruptions counts.
type BudgetMap map[string]map[v1alpha1.DisruptionReason]int

// Allowed returns the remaining budget for the given pool/reason. Zero when
// unset.
func (b BudgetMap) Allowed(poolName string, reason v1alpha1.DisruptionReason) int {
	pm, ok := b[poolName]
	if !ok {
		return 0
	}
	return pm[reason]
}

// Set writes a budget value for the given pool/reason.
func (b BudgetMap) Set(poolName string, reason v1alpha1.DisruptionReason, n int) {
	pm, ok := b[poolName]
	if !ok {
		pm = map[v1alpha1.DisruptionReason]int{}
		b[poolName] = pm
	}
	pm[reason] = n
}
