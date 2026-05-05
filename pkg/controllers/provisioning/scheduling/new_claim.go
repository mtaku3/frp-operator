package scheduling

import (
	"crypto/sha256"
	"encoding/hex"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// NewClaimFromPool builds an *InflightClaim from a pool template.
// Name = <pool-name>-<8-char-hex(sha256(pool|salt))>. Within one Solve
// (same salt) the same input produces the same name; across Solves the
// salt should differ so retries don't collide on apiserver Create.
func NewClaimFromPool(pool *v1alpha1.ExitPool, salt string) *InflightClaim {
	sum := sha256.Sum256([]byte(pool.Name + "|" + salt))
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
