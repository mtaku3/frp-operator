package operator_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/operator"
)

var _ = Describe("indexers", func() {
	var (
		k8s    client.Client
		mgrCtx context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(operator.SetupIndexersForTest(context.Background(), mgr)).To(Succeed())

		mgrCtx, cancel = context.WithCancel(context.Background())
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()

		k8s = mgr.GetClient()
		Eventually(func() error {
			var l v1alpha1.ExitClaimList
			return k8s.List(mgrCtx, &l)
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	AfterEach(func() {
		ctx := context.Background()
		var ecs v1alpha1.ExitClaimList
		_ = k8s.List(ctx, &ecs)
		for i := range ecs.Items {
			_ = k8s.Delete(ctx, &ecs.Items[i])
		}
		var tns v1alpha1.TunnelList
		_ = k8s.List(ctx, &tns)
		for i := range tns.Items {
			_ = k8s.Delete(ctx, &tns.Items[i])
		}
		cancel()
	})

	It("queries ExitClaim by status.providerID", func() {
		ec := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "ec-1"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "FakeProviderClass", Name: "default"},
				Frps: v1alpha1.FrpsConfig{
					Version:    "0.62.0",
					AllowPorts: []string{"7000-8000"},
					Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
				},
				Resources: v1alpha1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
		}
		Expect(k8s.Create(mgrCtx, ec)).To(Succeed())
		ec.Status.ProviderID = "fake://abc"
		Expect(k8s.Status().Update(mgrCtx, ec)).To(Succeed())

		Eventually(func() int {
			var l v1alpha1.ExitClaimList
			Expect(k8s.List(mgrCtx, &l, client.MatchingFields{operator.IndexExitClaimProviderID: "fake://abc"})).To(Succeed())
			return len(l.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(1))
	})

	It("queries ExitClaim by spec.providerClassRef.name", func() {
		ec := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "ec-2"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "FakeProviderClass", Name: "myclass"},
				Frps: v1alpha1.FrpsConfig{
					Version:    "0.62.0",
					AllowPorts: []string{"7000-8000"},
					Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
				},
			},
		}
		Expect(k8s.Create(mgrCtx, ec)).To(Succeed())

		Eventually(func() int {
			var l v1alpha1.ExitClaimList
			Expect(k8s.List(mgrCtx, &l, client.MatchingFields{operator.IndexExitClaimProviderClassRef: "myclass"})).To(Succeed())
			return len(l.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(1))
	})

	It("queries Tunnel by status.assignedExit", func() {
		t := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "t-1", Namespace: "default"},
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8s.Create(mgrCtx, t)).To(Succeed())
		t.Status.AssignedExit = "ec-bound"
		Expect(k8s.Status().Update(mgrCtx, t)).To(Succeed())

		Eventually(func() int {
			var l v1alpha1.TunnelList
			Expect(k8s.List(mgrCtx, &l, client.MatchingFields{operator.IndexTunnelAssignedExit: "ec-bound"})).To(Succeed())
			return len(l.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(1))
	})
})
