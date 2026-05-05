/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package exitclaim provides List / Get / WaitForCondition helpers for
// the cluster-scoped ExitClaim CR.
package exitclaim

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// List returns every ExitClaim in the cluster. The ns argument is
// retained for symmetry with the namespaced helpers but is ignored —
// ExitClaim is cluster-scoped.
func List(ctx context.Context, c client.Client, _ string) ([]v1alpha1.ExitClaim, error) {
	var list v1alpha1.ExitClaimList
	if err := c.List(ctx, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ListForPool filters by LabelExitPool=poolName.
func ListForPool(ctx context.Context, c client.Client, poolName string) ([]v1alpha1.ExitClaim, error) {
	all, err := List(ctx, c, "")
	if err != nil {
		return nil, err
	}
	out := make([]v1alpha1.ExitClaim, 0, len(all))
	for _, e := range all {
		if e.Labels[v1alpha1.LabelExitPool] == poolName {
			out = append(out, e)
		}
	}
	return out, nil
}

// Get returns the cluster-scoped ExitClaim by name.
func Get(ctx context.Context, c client.Client, name string) (*v1alpha1.ExitClaim, error) {
	var e v1alpha1.ExitClaim
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// WaitForCondition polls until the named condition reaches the wanted
// status (e.g. ConditionTypeReady = True) or the timeout elapses.
func WaitForCondition(
	ctx context.Context,
	c client.Client,
	name, condType string,
	want metav1.ConditionStatus,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var lastStatus metav1.ConditionStatus
	for {
		ec, err := Get(ctx, c, name)
		if err == nil {
			for _, cond := range ec.Status.Conditions {
				if cond.Type == condType {
					lastStatus = cond.Status
					if cond.Status == want {
						return nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ExitClaim %q condition %q never reached %q (last=%q): %w",
				name, condType, want, lastStatus, err)
		}
		time.Sleep(2 * time.Second)
	}
}
