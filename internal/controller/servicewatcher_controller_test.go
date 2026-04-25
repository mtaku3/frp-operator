package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("ServiceWatcherController", func() {
	ctx := context.Background()
	var recon *ServiceWatcherReconciler

	BeforeEach(func() {
		recon = &ServiceWatcherReconciler{Client: k8sClient, Scheme: scheme.Scheme}
	})

	It("creates a Tunnel for a matching Service", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-1", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("frp-operator.io/frp"),
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				},
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-1", Namespace: "default"}}
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var t frpv1alpha1.Tunnel
		Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
		Expect(t.Spec.Service.Name).To(Equal("sw-1"))
		Expect(t.Spec.Ports).To(HaveLen(1))
		Expect(t.Spec.Ports[0].ServicePort).To(Equal(int32(80)))
		// Tunnel is owned by Service.
		Expect(t.OwnerReferences).To(HaveLen(1))
		Expect(t.OwnerReferences[0].Kind).To(Equal("Service"))
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })
	})

	It("ignores Services without the matching class", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-other", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("other-vendor/lb"),
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				},
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-other", Namespace: "default"}}
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// No Tunnel created.
		var t frpv1alpha1.Tunnel
		err = k8sClient.Get(ctx, req.NamespacedName, &t)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("syncs assigned IP into Service.status.loadBalancer.ingress", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-3", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("frp-operator.io/frp"),
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				},
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-3", Namespace: "default"}}
		// First reconcile: create Tunnel.
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Simulate TunnelController having advanced status to Connecting+IP.
		var t frpv1alpha1.Tunnel
		Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })
		t.Status.Phase = frpv1alpha1.TunnelConnecting
		t.Status.AssignedIP = "203.0.113.42"
		t.Status.AssignedExit = "exit-x"
		t.Status.AssignedPorts = []int32{80}
		Expect(k8sClient.Status().Update(ctx, &t)).To(Succeed())

		// Second reconcile: should patch Service.status.
		_, err = recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got corev1.Service
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.LoadBalancer.Ingress).To(HaveLen(1))
		Expect(got.Status.LoadBalancer.Ingress[0].IP).To(Equal("203.0.113.42"))
	})

	It("updates Tunnel.spec when annotations change", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "sw-4", Namespace: "default",
				Annotations: map[string]string{"frp-operator.io/region": "nyc1"},
			},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("frp-operator.io/frp"),
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				},
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-4", Namespace: "default"}}
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var t frpv1alpha1.Tunnel
		Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
		Expect(t.Spec.Placement).NotTo(BeNil())
		Expect(t.Spec.Placement.Regions).To(Equal([]string{"nyc1"}))
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })

		// Update annotation.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sw-4", Namespace: "default"}, svc)).To(Succeed())
		svc.Annotations["frp-operator.io/region"] = "sfo3"
		Expect(k8sClient.Update(ctx, svc)).To(Succeed())

		_, err = recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
		Expect(t.Spec.Placement.Regions).To(Equal([]string{"sfo3"}))
	})
})
