/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

package tunnel

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the Tunnel by namespaced name.
func Get(ctx context.Context, c client.Client, ns, name string) (*frpv1alpha1.Tunnel, error) {
	var t frpv1alpha1.Tunnel
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// WaitForPhase polls the Tunnel's status.phase until it equals want
// or the timeout elapses.
func WaitForPhase(ctx context.Context, c client.Client, ns, name string, want frpv1alpha1.TunnelPhase, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		t, err := Get(ctx, c, ns, name)
		if err == nil && t.Status.Phase == want {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}
