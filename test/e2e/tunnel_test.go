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
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils/exitserver"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Tunnel lifecycle", Ordered, func() {
	const ns = "default"
	const tunnelName = "tunnel-basic"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())
	})

	AfterAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml)

		// Wait for tunnel/exitserver list to drain so the next Describe
		// doesn't see ghosts from this one.
		Eventually(func() int {
			ts, _ := listTunnels(ns)
			es, _ := exitserver.List(context.Background(), k8sClient, ns)
			return len(ts) + len(es)
		}, 2*time.Minute, 2*time.Second).Should(Equal(0))
	})

	It("ServiceWatcher creates a sibling Tunnel that reaches Ready", func() {
		Expect(tunnel.WaitForPhase(context.Background(), k8sClient, ns, tunnelName,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())
	})
})

func listTunnels(ns string) ([]frpv1alpha1.Tunnel, error) {
	var list frpv1alpha1.TunnelList
	if err := k8sClient.List(context.Background(), &list); err != nil {
		return nil, err
	}
	out := make([]frpv1alpha1.Tunnel, 0, len(list.Items))
	for _, t := range list.Items {
		if t.Namespace == ns {
			out = append(out, t)
		}
	}
	return out, nil
}
