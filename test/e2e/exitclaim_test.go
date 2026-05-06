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

var _ = Describe("ExitClaim finalizer + lifecycle", Ordered, func() {
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

	It("drains the docker container when ExitClaim is deleted", func() {
		// Capture the assigned ExitClaim and the underlying docker
		// container ID. The stable-salt naming scheme gives the next
		// (replacement) claim the same Name as the deleted one, so we
		// must verify the *original* container (by providerID) is gone,
		// not just by Name.
		var assigned, originalContainerID string
		Eventually(func(g Gomega) {
			t, err := tunnelutil.Get(suiteCtx, k8sClient, ns, tunnelName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(t.Status.Phase).To(Equal(v1alpha1.TunnelPhaseReady))
			g.Expect(t.Status.AssignedExit).NotTo(BeEmpty())
			assigned = t.Status.AssignedExit

			ec, err := exitclaimutil.Get(suiteCtx, k8sClient, assigned)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ec.Status.ProviderID).NotTo(BeEmpty())
			originalContainerID = strings.TrimPrefix(ec.Status.ProviderID, "localdocker://")
			g.Expect(originalContainerID).NotTo(BeEmpty())
		}, 4*time.Minute, 5*time.Second).Should(Succeed())

		ec, err := exitclaimutil.Get(suiteCtx, k8sClient, assigned)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(suiteCtx, ec)).To(Succeed())

		// The original container must be removed. A replacement may
		// (correctly) appear with the same human-readable name but a
		// different container ID.
		Eventually(func() bool {
			out, _ := utils.Run(exec.Command("docker", "ps", "-a", "--format", "{{.ID}}",
				"--filter", "id="+originalContainerID))
			return strings.TrimSpace(out) == ""
		}, 2*time.Minute, 5*time.Second).Should(BeTrue(),
			"docker container %s for ExitClaim %s should be removed", originalContainerID, assigned)
	})

	PIt("rejects mutation of immutable fields when claim is Ready (deferred to v1beta1 status-validation)", func() {
		// ImmutableWhenReady is moved to status validation in Phase 9
		// and will be e2e-spec'd in v1beta1.
	})
})
