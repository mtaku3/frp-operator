//go:build e2e

/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	exitclaimutil "github.com/mtaku3/frp-operator/test/utils/exitclaim"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	tunnelutil "github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Reverse-sync (ExitClaim PublicIP -> Service.Status.LoadBalancer.Ingress)", Ordered, func() {
	const (
		ns         = "default"
		tunnelName = "tunnel-basic"
	)

	BeforeAll(func() {
		yaml, err := os.ReadFile(filepath.Join("fixtures", "tunnel_basic.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(suiteCtx, yaml)).To(Succeed())
	})

	AfterAll(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "-f",
			filepath.Join("fixtures", "tunnel_basic.yaml"),
			"--ignore-not-found", "--wait=false"))
	})

	It("populates Service.Status.LoadBalancer.Ingress[0].IP from the ExitClaim's PublicIP", func() {
		Eventually(func(g Gomega) {
			t, err := tunnelutil.Get(suiteCtx, k8sClient, ns, tunnelName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(t.Status.Phase).To(Equal(v1alpha1.TunnelPhaseReady))
			g.Expect(t.Status.AssignedExit).NotTo(BeEmpty())

			ec, err := exitclaimutil.Get(suiteCtx, k8sClient, t.Status.AssignedExit)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ec.Status.PublicIP).NotTo(BeEmpty())

			var svc corev1.Service
			g.Expect(k8sClient.Get(suiteCtx, types.NamespacedName{Namespace: ns, Name: tunnelName}, &svc)).To(Succeed())
			g.Expect(svc.Status.LoadBalancer.Ingress).NotTo(BeEmpty())
			g.Expect(svc.Status.LoadBalancer.Ingress[0].IP).To(Equal(ec.Status.PublicIP))
		}, 4*time.Minute, 5*time.Second).Should(Succeed())
	})
})
