package lifecycle_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
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
	cpfake "github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
)

var (
	cfg          *rest.Config
	k8sClient    client.Client
	testEnv      *envtest.Environment
	cancel       context.CancelFunc
	cpProv       *cpfake.CloudProvider
	adminSrv     *httptest.Server
	adminCalls   atomic.Int32
	adminVersion atomic.Value // string
)

func TestLifecycle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "exitclaim lifecycle suite")
}

var _ = BeforeSuite(func() {
	By("starting envtest")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
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

	registry := cloudprovider.NewRegistry()
	cpProv = cpfake.New()
	Expect(registry.Register("FakeProviderClass", cpProv)).To(Succeed())

	// httptest server stands in for frps admin API across all suite tests.
	adminVersion.Store("v0.68.1")
	adminSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		adminCalls.Add(1)
		v, _ := adminVersion.Load().(string)
		_, _ = w.Write([]byte(`{"version":"` + v + `","bindPort":7000}`))
	}))

	adminFactory := func(_ string) *admin.Client { return admin.New(adminSrv.URL) }

	ctlr := lifecycle.New(mgr.GetClient(), registry, adminFactory)
	Expect(ctlr.SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterEach(func() {
	ctx := context.Background()
	cpProv.Reset()
	var claims v1alpha1.ExitClaimList
	_ = k8sClient.List(ctx, &claims)
	for i := range claims.Items {
		// Strip finalizer so envtest cleans up.
		c := &claims.Items[i]
		if len(c.Finalizers) > 0 {
			c.Finalizers = nil
			_ = k8sClient.Update(ctx, c)
		}
		_ = k8sClient.Delete(ctx, c)
	}
	var tunnels v1alpha1.TunnelList
	_ = k8sClient.List(ctx, &tunnels)
	for i := range tunnels.Items {
		_ = k8sClient.Delete(ctx, &tunnels.Items[i])
	}
})

var _ = AfterSuite(func() {
	By("stopping manager + envtest")
	if cancel != nil {
		cancel()
	}
	if adminSrv != nil {
		adminSrv.Close()
	}
	Expect(testEnv.Stop()).To(Succeed())
})
