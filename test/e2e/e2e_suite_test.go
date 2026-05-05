//go:build e2e

/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package e2e is the Ginkgo end-to-end suite. The build tag `e2e`
// keeps the suite out of `go test ./...` for unit-test runs; CI flips
// the tag in Makefile target `test-e2e`.
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/operator"
)

const (
	managerImage = "example.com/frp-operator:v0.0.1"
	overlayPath  = "config/overlays/e2e"
)

var (
	suiteCtx  context.Context
	k8sClient client.Client
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "frp-operator e2e suite")
}

var _ = BeforeSuite(func() {
	suiteCtx = context.Background()

	By("registering CRDs in the scheme")
	Expect(v1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ldv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(dov1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("building and loading the manager image")
	_, err := utils.Run(exec.Command("make", "docker-build", "IMG="+managerImage))
	Expect(err).NotTo(HaveOccurred())
	cluster := os.Getenv("KIND_CLUSTER")
	if cluster == "" {
		cluster = "frp-operator-test-e2e"
	}
	_, err = utils.Run(exec.Command("kind", "load", "docker-image", managerImage, "--name", cluster))
	Expect(err).NotTo(HaveOccurred())

	By("applying the e2e overlay")
	_, err = utils.Run(exec.Command("kubectl", "apply", "-k", overlayPath, "--server-side", "--force-conflicts"))
	Expect(err).NotTo(HaveOccurred())

	cfg := ctrl.GetConfigOrDie()
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	By("waiting for the operator to become Ready")
	Expect(operator.WaitForOperatorReady(suiteCtx, k8sClient, 5*time.Minute)).To(Succeed())

	By("applying shared ExitPool + ProviderClass + credentials")
	yaml, err := os.ReadFile(filepath.Join("fixtures", "shared.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(kubernetes.ApplyServerSide(suiteCtx, yaml)).To(Succeed())
})

var _ = AfterSuite(func() {
	By("deleting the e2e overlay")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "-k", overlayPath,
		"--ignore-not-found", "--wait=false"))
})
