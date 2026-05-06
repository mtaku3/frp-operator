package methods

import (
	"context"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// Drift selects exits whose pool template hash no longer matches the pool's
// current hash. Phase 7 stamps both annotations; until then the annotations
// are absent and Drift returns nothing — safe by design.
type Drift struct{}

func NewDrift() *Drift                             { return &Drift{} }
func (m *Drift) Name() string                      { return "Drift" }
func (m *Drift) Reason() v1alpha1.DisruptionReason { return v1alpha1.DisruptionReasonDrifted }
func (m *Drift) Forceful() bool                    { return false }

func (m *Drift) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
	if c == nil || c.Claim == nil || c.Pool == nil || c.State == nil {
		return false
	}
	if c.State.IsMarkedForDeletion() {
		return false
	}
	if hashesDiffer(c.Claim, c.Pool, v1alpha1.AnnotationPoolHash) {
		return true
	}
	if hashesDiffer(c.Claim, c.Pool, v1alpha1.AnnotationProviderClassHash) {
		return true
	}
	return false
}

// hashesDiffer reports whether claim and pool both carry the named
// hash annotation and the values differ. Missing on either side is
// treated as "not drifted yet" so a hash controller that hasn't
// caught up doesn't churn replacements.
func hashesDiffer(claim, pool interface {
	GetAnnotations() map[string]string
}, key string) bool {
	a := claim.GetAnnotations()[key]
	b := pool.GetAnnotations()[key]
	if a == "" || b == "" {
		return false
	}
	return a != b
}

func (m *Drift) ComputeCommands(
	_ context.Context,
	budgets disruption.BudgetMap,
	candidates ...*disruption.Candidate,
) ([]*disruption.Command, error) {
	return computePerPoolWithReplacements(m.Name(), v1alpha1.DisruptionReasonDrifted, budgets, candidates)
}

// computePerPoolWithReplacements buckets candidates by pool, caps each bucket
// by the budget for `reason`, and emits one Command per pool that includes a
// like-for-like replacement claim per candidate. The replacement is stamped
// with the pool's current template hash so the new claim is "fresh".
func computePerPoolWithReplacements(
	method string,
	reason v1alpha1.DisruptionReason,
	budgets disruption.BudgetMap,
	candidates []*disruption.Candidate,
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
		allowed := budgets.Allowed(poolName, reason)
		if allowed <= 0 {
			continue
		}
		if len(cs) > allowed {
			cs = cs[:allowed]
		}
		replacements := make([]*v1alpha1.ExitClaim, 0, len(cs))
		for _, c := range cs {
			replacements = append(replacements, replacementForCandidate(c))
		}
		out = append(out, &disruption.Command{
			Candidates:   cs,
			Replacements: replacements,
			Reason:       reason,
			Method:       method,
		})
	}
	return out, nil
}
