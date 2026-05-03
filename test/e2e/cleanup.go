//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/test/utils/exitserver"
)

// drainNamespace removes any leftover Tunnels and ExitServers from ns and
// waits for both lists to be empty. Reclaim is disabled in e2e (so empty
// exits don't get auto-destroyed mid-test); each Describe's AfterAll
// must call this to leave the cluster clean for the next Describe.
func drainNamespace(ctx context.Context, ns string) {
	ts, _ := listTunnels(ns)
	for i := range ts {
		_ = k8sClient.Delete(ctx, &ts[i])
	}
	es, _ := exitserver.List(ctx, k8sClient, ns)
	for i := range es {
		_ = k8sClient.Delete(ctx, &es[i], &client.DeleteOptions{})
	}
	Eventually(func() int {
		t, _ := listTunnels(ns)
		e, _ := exitserver.List(ctx, k8sClient, ns)
		return len(t) + len(e)
	}, 3*time.Minute, 2*time.Second).Should(Equal(0))
}
