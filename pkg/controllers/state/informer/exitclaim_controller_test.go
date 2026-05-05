package informer_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

var _ = Describe("ExitClaim informer", func() {
	It("propagates Create + ProviderID into Cluster", func() {
		ctx := context.Background()
		claim := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "informer-e1"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, claim) }()

		claim.Status.ProviderID = "fake://informer-id"
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())

		Eventually(func() *state.StateExit {
			return cluster.ExitForName("informer-e1")
		}, "5s", "200ms").ShouldNot(BeNil())
	})

	It("removes ExitClaim from Cluster on Delete", func() {
		ctx := context.Background()
		claim := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "informer-e2"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		claim.Status.ProviderID = "fake://informer-id-2"
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())

		Eventually(func() *state.StateExit {
			return cluster.ExitForName("informer-e2")
		}, "5s", "200ms").ShouldNot(BeNil())

		Expect(k8sClient.Delete(ctx, claim)).To(Succeed())

		Eventually(func() *state.StateExit {
			return cluster.ExitForName("informer-e2")
		}, "5s", "200ms").Should(BeNil())
	})
})
