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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils/exitserver"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("ServiceWatcher reverse-sync", Ordered, func() {
	const ns = "default"
	const svc = "rsync-svc"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/reverse_sync.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())
	})

	AfterAll(func() {
		yaml, err := os.ReadFile("fixtures/reverse_sync.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml)

		Eventually(func() int {
			ts, _ := listTunnels(ns)
			es, _ := exitserver.List(context.Background(), k8sClient, ns)
			return len(ts) + len(es)
		}, 3*time.Minute, 2*time.Second).Should(Equal(0))
	})

	It("reflects the assigned ExitServer.publicIP into Service.status", func() {
		ctx := context.Background()

		By("waiting for the Tunnel to reach Ready")
		Expect(tunnel.WaitForPhase(ctx, k8sClient, ns, svc,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())

		By("reading assigned exit and its publicIP")
		t, err := tunnel.Get(ctx, k8sClient, ns, svc)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Status.AssignedExit).NotTo(BeEmpty())

		exit, err := exitserver.Get(ctx, k8sClient, ns, t.Status.AssignedExit)
		Expect(err).NotTo(HaveOccurred())
		Expect(exit.Status.PublicIP).NotTo(BeEmpty())
		exitIP := exit.Status.PublicIP

		By("waiting for Service.status.loadBalancer.ingress[0].ip to equal the exit's publicIP")
		Eventually(func() string {
			var s corev1.Service
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: svc}, &s); err != nil {
				return ""
			}
			if len(s.Status.LoadBalancer.Ingress) == 0 {
				return ""
			}
			return s.Status.LoadBalancer.Ingress[0].IP
		}, 2*time.Minute, 2*time.Second).Should(Equal(exitIP))
	})
})
