package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("CRD install", func() {
	ctx := context.Background()

	It("creates and reads back an ExitServer", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-1", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "do-token", Key: "token"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.65.0"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())

		got := &frpv1alpha1.ExitServer{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "exit-1", Namespace: "default"}, got)
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		Expect(got.Spec.Provider).To(Equal(frpv1alpha1.ProviderDigitalOcean))
	})

	It("rejects an ExitServer with an invalid provider enum", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-bad", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       "not-a-real-provider",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "do-token", Key: "token"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.65.0"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		err := k8sClient.Create(ctx, exit)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected validation rejection, got %v", err)
	})

	It("creates a Tunnel with default migrationPolicy", func() {
		tn := &frpv1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "tn-1", Namespace: "default"},
			Spec: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc", Namespace: "default"},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80},
				},
				SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "default"},
			},
		}
		Expect(k8sClient.Create(ctx, tn)).To(Succeed())

		got := &frpv1alpha1.Tunnel{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tn-1", Namespace: "default"}, got)).To(Succeed())
		Expect(got.Spec.MigrationPolicy).To(Equal(frpv1alpha1.MigrationNever))
	})

	It("creates a cluster-scoped SchedulingPolicy", func() {
		sp := &frpv1alpha1.SchedulingPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
			Spec:       frpv1alpha1.SchedulingPolicySpec{},
		}
		Expect(k8sClient.Create(ctx, sp)).To(Succeed())
		got := &frpv1alpha1.SchedulingPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, got)).To(Succeed())
		Expect(string(got.Spec.Allocator)).To(Equal("CapacityAware"))
		Expect(string(got.Spec.Provisioner)).To(Equal("OnDemand"))
	})
})
