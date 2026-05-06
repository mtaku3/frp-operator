package scheduling

import (
	"crypto/sha256"
	"encoding/hex"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// NewClaimFromPool builds an *InflightClaim from a pool template.
// Name = <pool-name>-<8-char-hex(sha256(pool|tunnelUID))>. Because
// tunnelUID is set by the apiserver at Tunnel creation and is stable
// for the tunnel's lifetime, the same tunnel always produces the same
// claim name across Solves. That makes the AlreadyExists swallow in
// persistResults actually idempotent across Solves: a retried Create
// hits the same name and is rejected at the apiserver, instead of
// minting a duplicate ExitClaim.
func NewClaimFromPool(pool *v1alpha1.ExitPool, tunnelUID string) *InflightClaim {
	sum := sha256.Sum256([]byte(pool.Name + "|" + tunnelUID))
	name := pool.Name + "-" + hex.EncodeToString(sum[:])[:8]
	tmpl := pool.Spec.Template.Spec
	spec := v1alpha1.ExitClaimSpec{
		ProviderClassRef:       tmpl.ProviderClassRef,
		Requirements:           append([]v1alpha1.NodeSelectorRequirementWithMinValues(nil), tmpl.Requirements...),
		Frps:                   tmpl.Frps,
		ExpireAfter:            tmpl.ExpireAfter,
		TerminationGracePeriod: tmpl.TerminationGracePeriod,
	}
	return &InflightClaim{Spec: spec, Name: name, Pool: pool, UsedPorts: map[int32]struct{}{}}
}
