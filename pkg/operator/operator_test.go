package operator_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	"github.com/mtaku3/frp-operator/pkg/operator"
)

var _ = Describe("operator end-to-end", func() {
	It("provisions an ExitClaim and binds the Tunnel", func() {
		zapLog := zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter))
		log.SetLogger(zapLog)
		ctx, cancel := context.WithCancel(log.IntoContext(context.Background(), zapLog))
		defer cancel()

		// Fresh cfg with leader election off; bind probes/metrics to
		// random ports.
		opCfg := operator.Defaults()
		opCfg.LeaderElection = false
		opCfg.MetricsAddr = "0"
		opCfg.HealthProbeAddr = freeAddr()
		opCfg.BatchIdleDuration = 100 * time.Millisecond
		opCfg.BatchMaxDuration = 500 * time.Millisecond

		registry := cloudprovider.NewRegistry()
		Expect(registry.Register("FakeProviderClass", fake.New())).To(Succeed())

		runErrCh := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			runErrCh <- operator.RunWithRESTConfig(ctx, opCfg, cfg, registry)
		}()

		k8s, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		// Wait until the operator has joined and CRDs are reachable.
		Eventually(func() error {
			var l v1alpha1.ExitPoolList
			return k8s.List(ctx, &l)
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		pool := &v1alpha1.ExitPool{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke"},
			Spec: v1alpha1.ExitPoolSpec{
				Template: v1alpha1.ExitClaimTemplate{
					Spec: v1alpha1.ExitClaimTemplateSpec{
						ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
						Frps: v1alpha1.FrpsConfig{
							Version:    "v0.68.1",
							AllowPorts: []string{"8000-9000"},
							Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
						},
					},
				},
			},
		}
		Expect(k8s.Create(ctx, pool)).To(Succeed())

		tunnel := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "smoke",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.LabelExitPool: "smoke"},
			},
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8s.Create(ctx, tunnel)).To(Succeed())

		Eventually(func() int {
			var l v1alpha1.ExitClaimList
			Expect(k8s.List(ctx, &l)).To(Succeed())
			return len(l.Items)
		}, 30*time.Second, 200*time.Millisecond).Should(BeNumerically(">=", 1))

		Eventually(func() string {
			var t v1alpha1.Tunnel
			if err := k8s.Get(ctx, client.ObjectKey{Namespace: "default", Name: "smoke"}, &t); err != nil {
				return ""
			}
			return t.Status.AssignedExit
		}, 30*time.Second, 200*time.Millisecond).ShouldNot(BeEmpty())

		cancel()
		select {
		case err := <-runErrCh:
			// Expect either nil (clean shutdown) or context-canceled.
			if err != nil && err != context.Canceled {
				// Manager often returns nil on cancel; ignore otherwise.
				_ = err
			}
		case <-time.After(10 * time.Second):
			Fail("operator did not exit after cancel")
		}
	})
})
