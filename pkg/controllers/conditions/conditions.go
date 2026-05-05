// Package conditions provides race-safe helpers for upserting a single
// metav1.Condition on a Status.Conditions slice. The MergeFrom strategy
// used elsewhere in the operator emits a JSON merge patch that replaces
// the entire conditions array; if two controllers each compute their
// own condition off the same fresh read and patch concurrently, the
// later writer's patch overwrites the earlier writer's entry. This
// package emits a JSON Patch (RFC 6902) that targets a single index in
// /status/conditions, so concurrent writers updating different
// condition Types do not clobber each other.
package conditions

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MakeCondition builds a metav1.Condition for upsert into existing,
// preserving LastTransitionTime when the same Type already exists with
// the same Status. Callers should pass the freshly read
// pool.Status.Conditions as existing; the returned condition is the
// value to hand to PatchCondition.
func MakeCondition(existing []metav1.Condition, condType string, status metav1.ConditionStatus, observedGeneration int64, reason, message string) metav1.Condition {
	for _, c := range existing {
		if c.Type != condType {
			continue
		}
		if c.Status == status {
			return metav1.Condition{
				Type:               condType,
				Status:             status,
				LastTransitionTime: c.LastTransitionTime,
				ObservedGeneration: observedGeneration,
				Reason:             reason,
				Message:            message,
			}
		}
		break
	}
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
		Reason:             reason,
		Message:            message,
	}
}

// PatchCondition upserts newCond on obj's /status/conditions using a
// JSON Patch op so that concurrent writers updating *other* Types do
// not clobber each other.
//
// existing is the pre-mutation conditions slice (as last read from the
// API). It selects the op shape:
//
//   - When the Type already exists at index i, a `replace
//     /status/conditions/i` op rewrites only that index.
//   - When the Type is new and the slice is non-empty, an `add
//     /status/conditions/-` op appends without touching siblings.
//   - When the slice is empty/nil, the JSON Patch parent path doesn't
//     exist yet, so we fall back to a JSON Merge Patch that writes the
//     one-element array. This narrow case still races with another
//     writer doing the same first-init merge; once any writer has
//     committed once, subsequent writes use the race-safe ops above.
//     This trade keeps the helper compatible with apiservers (incl.
//     envtest) that reject `add` to a child of an absent CRD path.
func PatchCondition(ctx context.Context, c client.Client, obj client.Object, existing []metav1.Condition, newCond metav1.Condition) error {
	idx := -1
	for i, cur := range existing {
		if cur.Type == newCond.Type {
			idx = i
			break
		}
	}

	if idx < 0 && len(existing) == 0 {
		// First-init: parent path /status/conditions doesn't exist on
		// the stored object, so an `add` op cannot reach it. Use a
		// JSON Merge Patch to materialize the slice. After this round
		// trip, future writes go through the JSON Patch branch.
		mergeBody, err := json.Marshal(map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": []metav1.Condition{newCond},
			},
		})
		if err != nil {
			return err
		}
		return c.Status().Patch(ctx, obj, client.RawPatch(types.MergePatchType, mergeBody))
	}

	var op map[string]interface{}
	if idx >= 0 {
		op = map[string]interface{}{
			"op":    "replace",
			"path":  fmt.Sprintf("/status/conditions/%d", idx),
			"value": newCond,
		}
	} else {
		op = map[string]interface{}{
			"op":    "add",
			"path":  "/status/conditions/-",
			"value": newCond,
		}
	}
	body, err := json.Marshal([]map[string]interface{}{op})
	if err != nil {
		return err
	}
	return c.Status().Patch(ctx, obj, client.RawPatch(types.JSONPatchType, body))
}
