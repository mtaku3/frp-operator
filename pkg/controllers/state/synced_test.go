package state_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

var _ = Describe("Cluster.Synced", func() {
	It("returns true for empty cluster + empty cache", func() {
		c := state.NewCluster(k8sClient)
		Expect(c.Synced(context.Background())).To(BeTrue())
	})

	It("returns false when claim exists in API but not cache", func() {
		ctx := context.Background()
		claim := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "e-out-of-sync"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		// Patch ProviderID via status update.
		claim.Status.ProviderID = "fake://uut"
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, claim) }()

		c := state.NewCluster(k8sClient)
		Expect(c.Synced(ctx)).To(BeFalse())
	})
})
