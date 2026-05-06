package scheduling

import v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"

// Preferences peels off optional requirements one at a time on retry.
// Karpenter calls the equivalent scheduling.Preferences.Relax(pod). The
// v1 CRD has no preferred-vs-required distinction so Relax always
// returns false; Phase 6+ may introduce a `preferred` annotation.
type Preferences struct {
	Policy string // "Respect" | "Ignore"
}

// Relax drops one preferred requirement from the tunnel and returns true
// if anything was dropped. Always false in v1.
//
// TODO(phase4-followup): wire to a future PreferredRequirements field
// on TunnelSpec.
func (p *Preferences) Relax(_ *v1alpha1.Tunnel) bool {
	return false
}
