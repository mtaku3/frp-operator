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
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/tunnel"
)

const kindNodeName = "frp-operator-test-e2e-control-plane"

var _ = Describe("Traffic", Ordered, func() {
	const ns = "default"
	const svc = "tunnel-basic"
	const expectedBody = "tunnel-basic"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())
	})

	AfterAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml)

		drainNamespace(context.Background(), ns)
	})

	It("kind node curl through frps reaches the backend", func() {
		ctx := context.Background()

		By("waiting for the Tunnel to reach Ready")
		Expect(tunnel.WaitForPhase(ctx, k8sClient, ns, svc,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())

		By("resolving Service ingress IP")
		var ingressIP string
		Eventually(func() string {
			var s corev1.Service
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: svc}, &s); err != nil {
				return ""
			}
			if len(s.Status.LoadBalancer.Ingress) == 0 {
				return ""
			}
			ingressIP = s.Status.LoadBalancer.Ingress[0].IP
			return ingressIP
		}, 2*time.Minute, 2*time.Second).ShouldNot(BeEmpty())

		By("curl-ing the ingress from inside the kind node")
		Eventually(func() string {
			out, err := utils.Run(exec.Command("docker", "exec", kindNodeName,
				"curl", "-s", "--max-time", "5", "http://"+ingressIP+":80"))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(out)
		}, 2*time.Minute, 3*time.Second).Should(Equal(expectedBody))
	})
})
