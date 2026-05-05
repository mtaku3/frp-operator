package servicewatcher_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/servicewatcher"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	cancel    context.CancelFunc
)

func TestServicewatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "servicewatcher suite")
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

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	c := &servicewatcher.Controller{Client: mgr.GetClient()}
	Expect(c.SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterEach(func() {
	ctx := context.Background()
	var svcs corev1.ServiceList
	_ = k8sClient.List(ctx, &svcs)
	for i := range svcs.Items {
		s := &svcs.Items[i]
		if s.Namespace == "default" && s.Name == "kubernetes" {
			continue
		}
		if len(s.Finalizers) > 0 {
			s.Finalizers = nil
			_ = k8sClient.Update(ctx, s)
		}
		_ = k8sClient.Delete(ctx, s)
	}
	var tunnels v1alpha1.TunnelList
	_ = k8sClient.List(ctx, &tunnels)
	for i := range tunnels.Items {
		t := &tunnels.Items[i]
		if len(t.Finalizers) > 0 {
			t.Finalizers = nil
			_ = k8sClient.Update(ctx, t)
		}
		_ = k8sClient.Delete(ctx, t)
	}
})

var _ = AfterSuite(func() {
	By("stopping manager + envtest")
	if cancel != nil {
		cancel()
	}
	Expect(testEnv.Stop()).To(Succeed())
})
