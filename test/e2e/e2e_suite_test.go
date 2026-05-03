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
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/operator"
)

const (
	managerImage = "example.com/frp-operator:v0.0.1"
)

var (
	k8sClient client.Client
	suiteCtx  = context.Background()
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintln(GinkgoWriter, "frp-operator e2e suite")
	RunSpecs(t, "e2e")
}

var _ = BeforeSuite(func() {
	By("registering CRDs in the scheme")
	Expect(frpv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	cfg := ctrl.GetConfigOrDie()
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	k8sClient = c

	By("building and loading the manager image")
	_, err = utils.Run(exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage)))
	Expect(err).NotTo(HaveOccurred())
	Expect(utils.LoadImageToKindClusterWithName(managerImage)).To(Succeed())

	By("applying the e2e overlay")
	_, err = utils.Run(exec.Command(
		"kubectl", "apply", "-k", "config/overlays/e2e",
		"--server-side", "--force-conflicts",
	))
	Expect(err).NotTo(HaveOccurred())

	By("waiting for the operator to become Ready (Deployment + cert + dry-run admission probe)")
	Expect(operator.WaitForReady(suiteCtx, k8sClient, 5*time.Minute, true)).To(Succeed())

	By("applying shared SchedulingPolicy and credentials Secret")
	yaml, err := os.ReadFile("fixtures/shared.yaml")
	Expect(err).NotTo(HaveOccurred())
	Expect(kubernetes.ApplyServerSide(suiteCtx, yaml)).To(Succeed())
})

var _ = AfterSuite(func() {
	if os.Getenv("KEEP_E2E_RESOURCES") == "1" {
		return
	}
	By("deleting the e2e overlay")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "-k", "config/overlays/e2e",
		"--ignore-not-found", "--wait=false"))
})
