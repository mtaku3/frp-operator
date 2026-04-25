package scheduler

// FixedPoolStrategy keeps the exit count at a fixed cap (Policy.spec.budget.maxExits).
// It NEVER provisions beyond that cap and DOES NOT pre-warm a pool — the
// "fixed" name reflects "never over the cap", not "always exactly N exits".
// Tunnels that arrive after the pool is full stay Pending until something
// frees up.
type FixedPoolStrategy struct{}

// Name implements ProvisionStrategy.
func (FixedPoolStrategy) Name() string { return "FixedPool" }

// Plan implements ProvisionStrategy. Identical to OnDemand under-budget,
// strict refusal otherwise.
func (FixedPoolStrategy) Plan(in ProvisionInput) (ProvisionDecision, error) {
	if reason := checkBudget(in); reason != "" {
		return ProvisionDecision{Reason: reason}, nil
	}
	return ProvisionDecision{Provision: true, Spec: composeSpec(in)}, nil
}
