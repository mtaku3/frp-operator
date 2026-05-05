// Package hash computes a canonical hash of an ExitPool's template and
// stamps it as an annotation on the pool and on every child ExitClaim.
// Phase 6 disruption.Drift compares pool.Annotations[AnnotationPoolHash]
// against claim.Annotations[AnnotationPoolHash] to decide drift.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PoolTemplateHash computes a canonical SHA-256 over the pool's
// Spec.Template.Spec. Requirements are sorted by Key (and Values within
// each requirement are sorted) before marshalling so the hash is
// independent of declaration order. Returns the leading 16 hex chars
// (64 bits) — collision probability is negligible at the per-pool scale
// and the short form keeps the annotation human-readable.
func PoolTemplateHash(pool *v1alpha1.ExitPool) (string, error) {
	tmpl := pool.Spec.Template.Spec.DeepCopy()
	canonicalize(tmpl)
	raw, err := json.Marshal(tmpl)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16], nil
}

// canonicalize sorts order-insensitive slices in place so semantically
// equal templates always serialize the same bytes.
func canonicalize(t *v1alpha1.ExitClaimTemplateSpec) {
	if t == nil {
		return
	}
	// Sort requirements by Key. Stable so duplicate-key (operator-distinct)
	// requirements preserve their relative order.
	sort.SliceStable(t.Requirements, func(i, j int) bool {
		return t.Requirements[i].Key < t.Requirements[j].Key
	})
	for i := range t.Requirements {
		sort.Strings(t.Requirements[i].Values)
	}
	// AllowPorts and ReservedPorts are spec-level, treat order-insensitive.
	sort.Strings(t.Frps.AllowPorts)
	sort.Slice(t.Frps.ReservedPorts, func(i, j int) bool {
		return t.Frps.ReservedPorts[i] < t.Frps.ReservedPorts[j]
	})
}
