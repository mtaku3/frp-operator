// Package hash computes a canonical hash of a ProviderClass's spec and
// stamps it as an annotation on the ProviderClass, on every ExitPool
// that references it, and on every child ExitClaim. Drift compares
// pool.Annotations[AnnotationProviderClassHash] against
// claim.Annotations[AnnotationProviderClassHash] to detect
// ProviderClass mutations — the karpenter NodeClass-hash analog
// (karpenter.sh/nodeclass-hash).
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SpecHash returns a SHA-256 over the object's Spec field, marshaled as
// JSON. All ProviderClass kinds in this operator follow the standard
// k8s convention of exposing a top-level Spec struct, so reflection
// finds it. Returns the leading 16 hex chars (64 bits) — same scheme
// as PoolTemplateHash for symmetry.
func SpecHash(obj client.Object) (string, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("providerclass hash: expected struct, got %s", v.Kind())
	}
	spec := v.FieldByName("Spec")
	if !spec.IsValid() {
		return "", fmt.Errorf("providerclass hash: %T has no Spec field", obj)
	}
	raw, err := json.Marshal(spec.Interface())
	if err != nil {
		return "", fmt.Errorf("providerclass hash: marshal spec: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16], nil
}
