package provisioning_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
	"github.com/mtaku3/frp-operator/pkg/controllers/state/informer"
)

var (
	cfg         *rest.Config
	k8sClient   client.Client
	testEnv     *envtest.Environment
	cluster     *state.Cluster
	provisioner *provisioning.Provisioner
	cancel      context.CancelFunc
)

func TestProvisioning(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "provisioning suite")
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

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	cluster = state.NewCluster(mgr.GetClient())

	registry := cloudprovider.NewRegistry()
	Expect(registry.Register("FakeProviderClass", fake.New())).To(Succeed())

	// Informer controllers feed the cluster cache.
	Expect((&informer.ExitClaimController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr)).To(Succeed())
	Expect((&informer.ExitPoolController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr)).To(Succeed())
	Expect((&informer.TunnelController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr)).To(Succeed())

	provisioner = provisioning.New(cluster, mgr.GetClient(), registry)
	Expect((&provisioning.PodController{Client: mgr.GetClient(), Batcher: provisioner.Batcher}).SetupWithManager(mgr)).To(Succeed())
	Expect((&provisioning.NodeController{Client: mgr.GetClient(), Batcher: provisioner.Batcher}).SetupWithManager(mgr)).To(Succeed())
	Expect(provisioner.SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
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
	// Drain any in-flight triggers so a follow-up test starts clean.
	_ = provisioner.Batcher.Drain()
})

var _ = AfterSuite(func() {
	By("stopping manager + envtest")
	if cancel != nil {
		cancel()
	}
	Expect(testEnv.Stop()).To(Succeed())
})
