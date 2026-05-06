package scheduling

import v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"

// Preferences peels off optional requirements one at a time on retry.
// Karpenter calls the equivalent scheduling.Preferences.Relax(pod);
// Solve loops "add → Relax → retry" until success or Relax exhaustion.
//
// v1alpha1 has no preferred-vs-required distinction on TunnelSpec, so
// Relax is a no-op (always returns false). The Solve loop is structured
// so introducing a TunnelSpec.PreferredRequirements field — and the
// corresponding drop-one-key body here — needs no scheduler changes.
type Preferences struct {
	Policy string // "Respect" | "Ignore"
}

// Relax drops one preferred requirement from the tunnel and returns
// true if anything was dropped. v1alpha1: always false.
func (p *Preferences) Relax(_ *v1alpha1.Tunnel) bool {
	return false
}
