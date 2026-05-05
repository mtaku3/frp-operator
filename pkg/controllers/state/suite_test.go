package state_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "state cluster suite")
}

var _ = BeforeSuite(func() {
	By("starting envtest")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(v1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ldv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(dov1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterEach(func() {
	ctx := context.Background()
	var claims v1alpha1.ExitClaimList
	_ = k8sClient.List(ctx, &claims)
	for i := range claims.Items {
		_ = k8sClient.Delete(ctx, &claims.Items[i])
	}
	var pools v1alpha1.ExitPoolList
	_ = k8sClient.List(ctx, &pools)
	for i := range pools.Items {
		_ = k8sClient.Delete(ctx, &pools.Items[i])
	}
	var tunnels v1alpha1.TunnelList
	_ = k8sClient.List(ctx, &tunnels)
	for i := range tunnels.Items {
		_ = k8sClient.Delete(ctx, &tunnels.Items[i])
	}
})

var _ = AfterSuite(func() {
	By("stopping envtest")
	Expect(testEnv.Stop()).To(Succeed())
})
