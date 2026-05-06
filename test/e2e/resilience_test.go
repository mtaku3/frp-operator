//go:build e2e

/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	exitclaimutil "github.com/mtaku3/frp-operator/test/utils/exitclaim"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	tunnelutil "github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Resilience: frpc reconnects after frps container restart", Ordered, func() {
	const (
		ns         = "default"
		tunnelName = "tunnel-basic"
		kindNode   = "frp-operator-test-e2e-control-plane"
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

	It("recovers traffic within 90s of frps container restart", func() {
		var (
			publicIP    string
			containerID string
		)

		Eventually(func(g Gomega) {
			t, err := tunnelutil.Get(suiteCtx, k8sClient, ns, tunnelName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(t.Status.Phase).To(Equal(v1alpha1.TunnelPhaseReady))
			g.Expect(t.Status.AssignedExit).NotTo(BeEmpty())

			ec, err := exitclaimutil.Get(suiteCtx, k8sClient, t.Status.AssignedExit)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ec.Status.PublicIP).NotTo(BeEmpty())
			publicIP = ec.Status.PublicIP
			containerID = t.Status.AssignedExit
		}, 4*time.Minute, 5*time.Second).Should(Succeed())

		out, err := utils.Run(exec.Command("docker", "ps", "--format", "{{.Names}}", "--filter",
			fmt.Sprintf("name=%s", containerID)))
		Expect(err).NotTo(HaveOccurred())
		name := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		Expect(name).NotTo(BeEmpty(), "expected a docker container matching the ExitClaim name")

		_, err = utils.Run(exec.Command("docker", "restart", name))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			url := fmt.Sprintf("http://%s:80", publicIP)
			_, err := utils.Run(exec.Command("docker", "exec", kindNode,
				"curl", "-sf", "--max-time", "5", url))
			return err
		}, 90*time.Second, 5*time.Second).Should(Succeed())
	})
})
