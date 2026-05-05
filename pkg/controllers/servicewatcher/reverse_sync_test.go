package servicewatcher_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("ServiceWatcher reverse-sync", func() {
	ctx := context.Background()

	It("populates Service.Status.LoadBalancer.Ingress from Tunnel.Status.AssignedIP", func() {
		svc := makeService("svc-rev", ourClass(), nil, []corev1.ServicePort{
			{Port: 80, TargetPort: intstr.FromInt(80)},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// Wait for sibling Tunnel created by forward Controller.
		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-rev"}, &t)).To(Succeed())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Patch Tunnel.Status.AssignedIP.
		var t v1alpha1.Tunnel
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-rev"}, &t)).To(Succeed())
		t.Status.AssignedIP = "10.0.0.5"
		Expect(k8sClient.Status().Update(ctx, &t)).To(Succeed())

		// Reverse-sync should bubble it onto the Service.Status.
		Eventually(func(g Gomega) {
			var fresh corev1.Service
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-rev"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.LoadBalancer.Ingress).To(HaveLen(1))
			g.Expect(fresh.Status.LoadBalancer.Ingress[0].IP).To(Equal("10.0.0.5"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("does not write Status when Tunnel.AssignedIP is empty", func() {
		svc := makeService("svc-rev-empty", ourClass(), nil, []corev1.ServicePort{
			{Port: 80, TargetPort: intstr.FromInt(80)},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-rev-empty"}, &t)).To(Succeed())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		Consistently(func() int {
			var fresh corev1.Service
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-rev-empty"}, &fresh)
			return len(fresh.Status.LoadBalancer.Ingress)
		}, 1*time.Second, 100*time.Millisecond).Should(Equal(0))
	})
})
