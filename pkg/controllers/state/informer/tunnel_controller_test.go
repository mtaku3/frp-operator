package informer_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

var _ = Describe("Tunnel informer", func() {
	It("propagates AssignedExit/AssignedPorts into Cluster bindings", func() {
		ctx := context.Background()
		ns := &metav1.ObjectMeta{Name: "tunnel-svc-a", Namespace: "default"}
		t := &v1alpha1.Tunnel{
			ObjectMeta: *ns,
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, t) }()

		t.Status.AssignedExit = "informer-target"
		t.Status.AssignedPorts = []int32{8080}
		Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())

		key := state.TunnelKey("default/tunnel-svc-a")
		Eventually(func() *state.TunnelBinding {
			return cluster.BindingForTunnel(key)
		}, "5s", "200ms").ShouldNot(BeNil())

		b := cluster.BindingForTunnel(key)
		Expect(b.ExitClaimName).To(Equal("informer-target"))
		Expect(b.AssignedPorts).To(ConsistOf(int32(8080)))
	})

	It("clears bindings when Tunnel is deleted", func() {
		ctx := context.Background()
		t := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "tunnel-svc-b", Namespace: "default"},
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		t.Status.AssignedExit = "informer-target"
		t.Status.AssignedPorts = []int32{9090}
		Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())

		key := state.TunnelKey("default/tunnel-svc-b")
		Eventually(func() *state.TunnelBinding {
			return cluster.BindingForTunnel(key)
		}, "5s", "200ms").ShouldNot(BeNil())

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())

		Eventually(func() *state.TunnelBinding {
			return cluster.BindingForTunnel(key)
		}, "5s", "200ms").Should(BeNil())
	})
})
