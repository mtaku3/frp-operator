package lifecycle_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func makeClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{
				Group: "frp.operator.io",
				Kind:  "FakeProviderClass",
				Name:  "default",
			},
			Frps: v1alpha1.FrpsConfig{
				Version:    "v0.68.1",
				BindPort:   7000,
				AdminPort:  7400,
				AllowPorts: []string{"1024-65535"},
				Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
			},
		},
	}
}

func makeTunnel(name, ns string) *v1alpha1.Tunnel {
	return &v1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1alpha1.TunnelSpec{
			Ports: []v1alpha1.TunnelPort{{ServicePort: 8080, Protocol: "TCP"}},
		},
	}
}

var _ = Describe("Controller", func() {
	ctx := context.Background()

	It("drives a fresh claim to Ready=True", func() {
		claim := makeClaim("happy")
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		Eventually(func(g Gomega) {
			got := &v1alpha1.ExitClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "happy"}, got)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(got.Status.Conditions, v1alpha1.ConditionTypeReady)).To(BeTrue())
			g.Expect(got.Status.ProviderID).NotTo(BeEmpty())
			g.Expect(got.Status.PublicIP).NotTo(BeEmpty())
			g.Expect(got.Finalizers).To(ContainElement(v1alpha1.TerminationFinalizer))
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("deletes a claim with no bound tunnels and strips finalizer", func() {
		claim := makeClaim("clean-delete")
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		Eventually(func(g Gomega) {
			got := &v1alpha1.ExitClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "clean-delete"}, got)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(got.Status.Conditions, v1alpha1.ConditionTypeReady)).To(BeTrue())
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		got := &v1alpha1.ExitClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "clean-delete"}, got)).To(Succeed())
		Expect(k8sClient.Delete(ctx, got)).To(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "clean-delete"}, &v1alpha1.ExitClaim{})
			return err != nil
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())

		// Provider should report the exit deleted.
		exits, err := cpProv.List(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(exits).To(BeEmpty())
	})

	It("waits to drain bound tunnels before deleting", func() {
		ns := &v1alpha1.ExitClaim{} // placeholder; tunnels live in default namespace
		_ = ns
		claim := makeClaim("with-tunnel")
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		Eventually(func(g Gomega) {
			got := &v1alpha1.ExitClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "with-tunnel"}, got)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(got.Status.Conditions, v1alpha1.ConditionTypeReady)).To(BeTrue())
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		tn := makeTunnel("t1", "default")
		Expect(k8sClient.Create(ctx, tn)).To(Succeed())
		// Bind tunnel to the claim via status.
		got := &v1alpha1.Tunnel{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "t1", Namespace: "default"}, got)).To(Succeed())
		got.Status.AssignedExit = "with-tunnel"
		got.Status.AssignedIP = "203.0.113.1"
		got.Status.AssignedPorts = []int32{30000}
		Expect(k8sClient.Status().Update(ctx, got)).To(Succeed())

		ec := &v1alpha1.ExitClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "with-tunnel"}, ec)).To(Succeed())
		Expect(k8sClient.Delete(ctx, ec)).To(Succeed())

		// AssignedExit gets cleared by finalize() so the scheduler can
		// rebind the tunnel. Verify that, then unbind permanently.
		Eventually(func(g Gomega) {
			t := &v1alpha1.Tunnel{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "t1", Namespace: "default"}, t)).To(Succeed())
			g.Expect(t.Status.AssignedExit).To(BeEmpty())
		}, 15*time.Second, 200*time.Millisecond).Should(Succeed())

		// Once unbound, claim should finalize and disappear.
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "with-tunnel"}, &v1alpha1.ExitClaim{})
			return err != nil
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})

	It("marks the exit for deletion in the cluster cache during finalize (issue #8)", func() {
		claim := makeClaim("mark-on-finalize")
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		Eventually(func(g Gomega) {
			got := &v1alpha1.ExitClaim{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mark-on-finalize"}, got)).To(Succeed())
			g.Expect(apimeta.IsStatusConditionTrue(got.Status.Conditions, v1alpha1.ConditionTypeReady)).To(BeTrue())
			// Seed the cache with the launched claim so MarkExitForDeletion
			// has something to flag. In production this is done by the
			// informer; the lifecycle suite skips informer wiring.
			testCluster.UpdateExit(got)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		got := &v1alpha1.ExitClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mark-on-finalize"}, got)).To(Succeed())
		Expect(k8sClient.Delete(ctx, got)).To(Succeed())

		Eventually(func(g Gomega) {
			se := testCluster.ExitForName("mark-on-finalize")
			g.Expect(se).NotTo(BeNil(), "claim should still be in cache mid-finalize")
			g.Expect(se.IsMarkedForDeletion()).To(BeTrue())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
