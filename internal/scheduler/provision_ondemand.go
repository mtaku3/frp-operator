package scheduler

import (
	"fmt"

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
	spec := composeSpec(in)
	// Refuse to provision an exit that wouldn't satisfy this tunnel: if
	// the policy's default AllowPorts doesn't cover every requested
	// public port, leave the tunnel Allocating so the user can widen
	// the policy or add a compatible exit manually.
	for _, p := range tunnelPorts(in.Tunnel) {
		if !portAllowed(spec.AllowPorts, p) {
			return ProvisionDecision{Reason: fmt.Sprintf(
				"tunnel port %d not in policy default AllowPorts %v", p, spec.AllowPorts,
			)}, nil
		}
	}
	return ProvisionDecision{Provision: true, Spec: spec}, nil
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
	// Seed AllowPorts from the policy default. Plan() refuses to
	// provision when the tunnel's requested ports fall outside this set.
	if len(def.AllowPorts) > 0 {
		spec.AllowPorts = append([]string(nil), def.AllowPorts...)
	} else {
		spec.AllowPorts = []string{"1024-65535"}
	}
	return spec
}

func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
