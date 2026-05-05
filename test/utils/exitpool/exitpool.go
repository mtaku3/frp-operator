/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package exitpool provides Get / Apply helpers for ExitPool CRs.
package exitpool

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the cluster-scoped ExitPool by name.
func Get(ctx context.Context, c client.Client, name string) (*v1alpha1.ExitPool, error) {
	var p v1alpha1.ExitPool
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Apply server-side applies pool through the controller-runtime client.
func Apply(ctx context.Context, c client.Client, pool *v1alpha1.ExitPool) error {
	body, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	return c.Patch(ctx, pool, client.RawPatch(types.ApplyPatchType, body),
		client.FieldOwner("e2e"), client.ForceOwnership)
}
