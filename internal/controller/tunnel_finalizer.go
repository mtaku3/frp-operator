/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// tunnelFinalizer is the finalizer string the controller adds to every
// Tunnel CR. Removed only after the Deployment, Secret, port reservation,
// and frps proxy entry have been cleaned up.
const tunnelFinalizer = "frp.operator.io/tunnel-finalizer"

// hasTunnelFinalizer reports whether the named finalizer is on the Tunnel.
func hasTunnelFinalizer(t *frpv1alpha1.Tunnel) bool {
	for _, f := range t.Finalizers {
		if f == tunnelFinalizer {
			return true
		}
	}
	return false
}

// addTunnelFinalizer appends the finalizer if missing and patches.
// Returns true if a patch was sent.
func addTunnelFinalizer(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	if hasTunnelFinalizer(t) {
		return false, nil
	}
	patch := client.MergeFrom(t.DeepCopy())
	t.Finalizers = append(t.Finalizers, tunnelFinalizer)
	if err := c.Patch(ctx, t, patch); err != nil {
		return false, fmt.Errorf("add tunnel finalizer: %w", err)
	}
	return true, nil
}

// removeTunnelFinalizer drops the finalizer and patches.
func removeTunnelFinalizer(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	if !hasTunnelFinalizer(t) {
		return false, nil
	}
	patch := client.MergeFrom(t.DeepCopy())
	out := t.Finalizers[:0]
	for _, f := range t.Finalizers {
		if f != tunnelFinalizer {
			out = append(out, f)
		}
	}
	t.Finalizers = out
	if err := c.Patch(ctx, t, patch); err != nil {
		return false, fmt.Errorf("remove tunnel finalizer: %w", err)
	}
	return true, nil
}
