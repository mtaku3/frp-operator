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

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	tunnelutil "github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Tunnel lifecycle", Ordered, func() {
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

	It("creates a Tunnel via ServiceWatcher and reaches Phase=Ready", func() {
		Eventually(func() (v1alpha1.TunnelPhase, error) {
			t, err := tunnelutil.Get(suiteCtx, k8sClient, ns, tunnelName)
			if err != nil {
				return "", err
			}
			return t.Status.Phase, nil
		}, 4*time.Minute, 5*time.Second).Should(Equal(v1alpha1.TunnelPhaseReady))
	})
})
