package scheduler

import (
	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// OnDemandStrategy provisions one new ExitServer when allocation fails,
// using the SchedulingPolicy.spec.vps.default settings. Tunnel-level
// placement preferences override the policy defaults.
type OnDemandStrategy struct{}

// Name implements ProvisionStrategy.
func (OnDemandStrategy) Name() string { return "OnDemand" }

// Plan implements ProvisionStrategy.
func (OnDemandStrategy) Plan(in ProvisionInput) (ProvisionDecision, error) {
	if reason := checkBudget(in); reason != "" {
		return ProvisionDecision{Reason: reason}, nil
	}
	return ProvisionDecision{Provision: true, Spec: composeSpec(in)}, nil
}

// checkBudget enforces the SchedulingPolicy.spec.budget caps. Returns a
// non-empty reason when the request must be refused; empty string when the
// budget allows another exit.
func checkBudget(in ProvisionInput) string {
	if in.Policy == nil {
		return ""
	}
	b := in.Policy.Spec.Budget
	if b.MaxExits != nil && int32(len(in.Current)) >= *b.MaxExits {
		return "BudgetExceeded: maxExits reached"
	}
	if b.MaxExitsPerNamespace != nil {
		ns := in.Tunnel.Namespace
		var count int32
		for _, e := range in.Current {
			if e.Namespace == ns {
				count++
			}
		}
		if count >= *b.MaxExitsPerNamespace {
			return "BudgetExceeded: maxExitsPerNamespace reached for " + ns
		}
	}
	return ""
}

// composeSpec produces the desired ExitServerSpec from the policy defaults
// and the tunnel's placement overrides.
func composeSpec(in ProvisionInput) frpv1alpha1.ExitServerSpec {
	def := in.Policy.Spec.VPS.Default
	spec := frpv1alpha1.ExitServerSpec{
		Provider: def.Provider,
		Region:   firstString(def.Regions),
		Size:     def.Size,
		Capacity: def.Capacity,
	}
	if in.Tunnel.Spec.Placement != nil {
		p := in.Tunnel.Spec.Placement
		if region := firstString(p.Regions); region != "" {
			spec.Region = region
		}
		if p.SizeOverride != "" {
			spec.Size = p.SizeOverride
		}
	}
	return spec
}

func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
