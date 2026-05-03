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

const (
	schedSvcA = "sched-a"
	schedSvcB = "sched-b"
	schedSvcC = "sched-c"
)

var schedSvcAYAML = []byte(`
apiVersion: v1
kind: Service
metadata: {name: sched-a, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: sched-a}
`)

var schedSvcBYAML = []byte(`
apiVersion: v1
kind: Service
metadata: {name: sched-b, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 81, targetPort: 8081, protocol: TCP}]
  selector: {app: sched-b}
`)

var schedSvcCYAML = []byte(`
apiVersion: v1
kind: Service
metadata: {name: sched-c, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: ssh, port: 22, targetPort: 22, protocol: TCP}]
  selector: {app: nonexistent}
`)

var _ = Describe("Scheduling", Ordered, func() {
	const ns = "default"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/scheduling.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())

		// Apply Service A first, wait for Tunnel A Ready, then apply
		// Service B — otherwise the two scheduler reconciles can race
		// and provision two exits instead of binpacking.
		Expect(kubernetes.ApplyServerSide(context.Background(), schedSvcAYAML)).To(Succeed())
		Expect(tunnel.WaitForPhase(context.Background(), k8sClient, ns, schedSvcA,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())
	})

	AfterAll(func() {
		_ = kubernetes.DeleteServerSide(context.Background(), schedSvcAYAML)
		_ = kubernetes.DeleteServerSide(context.Background(), schedSvcBYAML)
		_ = kubernetes.DeleteServerSide(context.Background(), schedSvcCYAML)
		yaml, err := os.ReadFile("fixtures/scheduling.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml)

		Eventually(func() int {
			ts, _ := listTunnels(ns)
			es, _ := exitserver.List(context.Background(), k8sClient, ns)
			return len(ts) + len(es)
		}, 3*time.Minute, 2*time.Second).Should(Equal(0))
	})

	It("schedules both tunnels onto a single ExitServer", func() {
		ctx := context.Background()

		By("applying Service B")
		Expect(kubernetes.ApplyServerSide(ctx, schedSvcBYAML)).To(Succeed())

		By("waiting for Tunnel B to reach Ready")
		Expect(tunnel.WaitForPhase(ctx, k8sClient, ns, schedSvcB,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())

		By("asserting both tunnels share the same assignedExit")
		ta, err := tunnel.Get(ctx, k8sClient, ns, schedSvcA)
		Expect(err).NotTo(HaveOccurred())
		tb, err := tunnel.Get(ctx, k8sClient, ns, schedSvcB)
		Expect(err).NotTo(HaveOccurred())
		Expect(ta.Status.AssignedExit).NotTo(BeEmpty())
		Expect(ta.Status.AssignedExit).To(Equal(tb.Status.AssignedExit))

		By("asserting the namespace lists exactly one ExitServer")
		exits, err := exitserver.List(ctx, k8sClient, ns)
		Expect(err).NotTo(HaveOccurred())
		Expect(exits).To(HaveLen(1))

		By("asserting both ports are allocated on that exit")
		Expect(exits[0].Status.Allocations).To(HaveKey("80"))
		Expect(exits[0].Status.Allocations).To(HaveKey("81"))
	})

	It("does not provision an exit when policy AllowPorts excludes the requested port", func() {
		ctx := context.Background()

		By("narrowing the policy default AllowPorts to exclude port 22")
		// Server-side apply the same policy with a tighter allowPorts.
		Expect(kubernetes.ApplyServerSide(ctx, []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: local-docker
      allowPorts: ["1024-65535"]
`))).To(Succeed())

		By("recording exit count before applying the new tunnel")
		exitsBefore, err := exitserver.List(ctx, k8sClient, "default")
		Expect(err).NotTo(HaveOccurred())
		exitCountBefore := len(exitsBefore)

		By("applying a Service requesting a sub-1024 port")
		Expect(kubernetes.ApplyServerSide(ctx, schedSvcCYAML)).To(Succeed())

		By("waiting for ServiceWatcher to create the Tunnel")
		Eventually(func() error {
			_, e := tunnel.Get(ctx, k8sClient, ns, schedSvcC)
			return e
		}, 30*time.Second, 2*time.Second).Should(Succeed())

		By("asserting the Tunnel stays in Allocating for 30s")
		Consistently(func() frpv1alpha1.TunnelPhase {
			t, e := tunnel.Get(ctx, k8sClient, ns, schedSvcC)
			if e != nil {
				return ""
			}
			return t.Status.Phase
		}, 30*time.Second, 5*time.Second).Should(Equal(frpv1alpha1.TunnelAllocating))

		By("asserting no new ExitServer was created")
		exitsAfter, err := exitserver.List(ctx, k8sClient, "default")
		Expect(err).NotTo(HaveOccurred())
		Expect(exitsAfter).To(HaveLen(exitCountBefore))

		By("cleaning up sched-c")
		_ = kubernetes.DeleteServerSide(ctx, schedSvcCYAML)
	})
})
