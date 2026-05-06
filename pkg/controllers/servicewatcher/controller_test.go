package servicewatcher_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/servicewatcher"
)

func ourClass() *string {
	s := servicewatcher.LoadBalancerClass
	return &s
}

func otherClass() *string {
	s := "example.com/other"
	return &s
}

func makeService(name string, lbClass *string, annotations map[string]string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: lbClass,
			Selector:          map[string]string{"app": name},
			Ports:             ports,
		},
	}
}

var _ = Describe("ServiceWatcher controller", func() {
	ctx := context.Background()

	It("creates a sibling Tunnel for our-class Service", func() {
		svc := makeService("svc-create", ourClass(), map[string]string{
			v1alpha1.AnnotationServiceCPURequest: "100m",
			v1alpha1.AnnotationServiceExitPool:   "shared",
			v1alpha1.AnnotationDoNotDisrupt:      "true",
		}, []corev1.ServicePort{
			{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-create"}, &t)).To(Succeed())
			g.Expect(t.Spec.Ports).To(HaveLen(1))
			g.Expect(t.Spec.Ports[0].PublicPort).NotTo(BeNil())
			g.Expect(*t.Spec.Ports[0].PublicPort).To(Equal(int32(80)))
			g.Expect(t.Spec.Ports[0].ServicePort).To(Equal(int32(8080)))
			g.Expect(t.Spec.Resources.Requests).To(HaveKey(corev1.ResourceCPU))
			g.Expect(t.Spec.Requirements).To(HaveLen(1))
			g.Expect(t.Spec.Requirements[0].Key).To(Equal(v1alpha1.LabelExitPool))
			g.Expect(t.Annotations).To(HaveKeyWithValue(v1alpha1.AnnotationDoNotDisrupt, "true"))
			g.Expect(t.Labels).To(HaveKeyWithValue(servicewatcher.LabelManagedByService, "true"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("ignores Service with another loadBalancerClass", func() {
		svc := makeService("svc-other", otherClass(), nil, []corev1.ServicePort{
			{Port: 80, TargetPort: intstr.FromInt(80)},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Consistently(func() bool {
			var t v1alpha1.Tunnel
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-other"}, &t)
			return apierrors.IsNotFound(err)
		}, 1*time.Second, 100*time.Millisecond).Should(BeTrue())
	})

	It("updates the Tunnel when Service annotations change", func() {
		svc := makeService("svc-upd", ourClass(), map[string]string{
			v1alpha1.AnnotationServiceCPURequest: "100m",
		}, []corev1.ServicePort{
			{Port: 80, TargetPort: intstr.FromInt(80)},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-upd"}, &t)).To(Succeed())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Refresh & mutate.
		var fresh corev1.Service
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-upd"}, &fresh)).To(Succeed())
		fresh.Annotations[v1alpha1.AnnotationServiceCPURequest] = "500m"
		fresh.Annotations[v1alpha1.AnnotationServiceExitPool] = "premium"
		Expect(k8sClient.Update(ctx, &fresh)).To(Succeed())

		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-upd"}, &t)).To(Succeed())
			cpu := t.Spec.Resources.Requests[corev1.ResourceCPU]
			g.Expect(cpu.String()).To(Equal("500m"))
			g.Expect(t.Spec.Requirements).To(HaveLen(1))
			g.Expect(t.Spec.Requirements[0].Values).To(ConsistOf("premium"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("deletes the sibling Tunnel when Service is deleted", func() {
		svc := makeService("svc-del", ourClass(), nil, []corev1.ServicePort{
			{Port: 80, TargetPort: intstr.FromInt(80)},
		})
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func(g Gomega) {
			var t v1alpha1.Tunnel
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-del"}, &t)).To(Succeed())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		Expect(k8sClient.Delete(ctx, svc)).To(Succeed())

		Eventually(func() bool {
			var t v1alpha1.Tunnel
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "svc-del"}, &t)
			return apierrors.IsNotFound(err)
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
	})
})
