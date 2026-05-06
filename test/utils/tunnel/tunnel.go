/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package tunnel provides Get / WaitForPhase helpers for Tunnel CRs.
package tunnel

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the Tunnel by namespaced name.
func Get(ctx context.Context, c client.Client, ns, name string) (*v1alpha1.Tunnel, error) {
	var t v1alpha1.Tunnel
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// WaitForPhase polls Tunnel.Status.Phase until it equals want.
func WaitForPhase(
	ctx context.Context,
	c client.Client,
	ns, name string,
	want v1alpha1.TunnelPhase,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	var lastPhase v1alpha1.TunnelPhase
	var lastErr error
	for {
		t, err := Get(ctx, c, ns, name)
		if err == nil {
			lastPhase = t.Status.Phase
			lastErr = nil
			if t.Status.Phase == want {
				return nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("tunnel %s/%s did not reach phase %q within %s: %w",
					ns, name, want, timeout, lastErr)
			}
			return fmt.Errorf("tunnel %s/%s did not reach phase %q within %s: last phase %q",
				ns, name, want, timeout, lastPhase)
		}
		time.Sleep(2 * time.Second)
	}
}

// List returns all Tunnels in ns.
func List(ctx context.Context, c client.Client, ns string) ([]v1alpha1.Tunnel, error) {
	var list v1alpha1.TunnelList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	return list.Items, nil
}
