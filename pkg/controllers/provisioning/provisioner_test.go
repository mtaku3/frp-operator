package provisioning_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func defaultPool(name string, allow []string) *v1alpha1.ExitPool {
	w := int32(10)
	return &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitPoolSpec{
			Weight: &w,
			Template: v1alpha1.ExitClaimTemplate{
				Spec: v1alpha1.ExitClaimTemplateSpec{
					ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
					Frps: v1alpha1.FrpsConfig{
						Version:    "v0.68.1",
						Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
						AllowPorts: allow,
					},
				},
			},
		},
	}
}

func newTunnel(name string, ports ...int32) *v1alpha1.Tunnel {
	tps := make([]v1alpha1.TunnelPort, 0, len(ports))
	for _, p := range ports {
		pp := p
		tps = append(tps, v1alpha1.TunnelPort{PublicPort: &pp, ServicePort: 8080, Protocol: "TCP"})
	}
	return &v1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec:       v1alpha1.TunnelSpec{Ports: tps},
	}
}

var _ = Describe("Provisioner integration", func() {
	It("creates an ExitClaim and patches Tunnel.Status when a pool exists", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, defaultPool("pool-a", []string{"80", "443"}))).To(Succeed())
		t := newTunnel("tn-a", 80)
		Expect(k8sClient.Create(ctx, t)).To(Succeed())

		Eventually(func() string {
			var got v1alpha1.Tunnel
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "tn-a"}, &got); err != nil {
				return ""
			}
			return got.Status.AssignedExit
		}, "20s", "200ms").ShouldNot(BeEmpty())

		var claims v1alpha1.ExitClaimList
		Expect(k8sClient.List(ctx, &claims)).To(Succeed())
		Expect(claims.Items).To(HaveLen(1))
		Expect(claims.Items[0].Labels[v1alpha1.LabelExitPool]).To(Equal("pool-a"))
	})

	It("binpacks two rapid Tunnels onto one ExitClaim", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, defaultPool("pool-b", []string{"80", "443"}))).To(Succeed())
		t1 := newTunnel("tn-b1", 80)
		t2 := newTunnel("tn-b2", 443)
		Expect(k8sClient.Create(ctx, t1)).To(Succeed())
		Expect(k8sClient.Create(ctx, t2)).To(Succeed())

		Eventually(func() bool {
			var t1got, t2got v1alpha1.Tunnel
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "tn-b1"}, &t1got); err != nil {
				return false
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "tn-b2"}, &t2got); err != nil {
				return false
			}
			return t1got.Status.AssignedExit != "" && t1got.Status.AssignedExit == t2got.Status.AssignedExit
		}, "20s", "200ms").Should(BeTrue())

		var claims v1alpha1.ExitClaimList
		Expect(k8sClient.List(ctx, &claims)).To(Succeed())
		Expect(claims.Items).To(HaveLen(1))
	})

	It("marks tunnel Unschedulable when no matching pool exists", func() {
		ctx := context.Background()
		t := newTunnel("tn-c", 80)
		Expect(k8sClient.Create(ctx, t)).To(Succeed())

		Eventually(func() (string, error) {
			var got v1alpha1.Tunnel
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "tn-c"}, &got); err != nil {
				return "", err
			}
			for _, c := range got.Status.Conditions {
				if c.Type == "Ready" {
					return c.Reason, nil
				}
			}
			return "", nil
		}, "20s", "200ms").Should(Equal("Unschedulable"))

		var claims v1alpha1.ExitClaimList
		Expect(k8sClient.List(ctx, &claims)).To(Succeed())
		Expect(claims.Items).To(BeEmpty())
	})
})
