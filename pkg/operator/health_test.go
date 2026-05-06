package operator_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/mtaku3/frp-operator/pkg/operator"
)

func freeAddr() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

var _ = Describe("health", func() {
	It("serves /healthz and /readyz with a passing CRD check", func() {
		probeAddr := freeAddr()

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: probeAddr,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(operator.SetupHealthChecksForTest(mgr)).To(Succeed())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(mgr.Start(ctx)).To(Succeed())
		}()

		urlHealth := fmt.Sprintf("http://%s/healthz", probeAddr)
		urlReady := fmt.Sprintf("http://%s/readyz", probeAddr)

		Eventually(func() int {
			resp, err := http.Get(urlHealth)
			if err != nil {
				return 0
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(http.StatusOK))

		Eventually(func() int {
			resp, err := http.Get(urlReady)
			if err != nil {
				return 0
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(http.StatusOK))
	})
})
