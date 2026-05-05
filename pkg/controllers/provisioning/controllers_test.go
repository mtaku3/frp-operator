package provisioning_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning"
)

// These tests construct a fresh controller wired to a private Batcher
// so the live provisioner loop in suite_test doesn't race against the
// observation. Direct Reconcile calls — no manager involved.

var _ = Describe("PodController", func() {
	It("triggers the batcher for an unscheduled tunnel", func() {
		ctx := context.Background()
		b := provisioning.NewBatcher[types.UID](500*time.Millisecond, 2*time.Second)
		c := &provisioning.PodController{Client: k8sClient, Batcher: b}

		port := int32(80)
		t := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "podctl-1"},
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{PublicPort: &port, ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())

		_, err := c.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "podctl-1"}})
		Expect(err).NotTo(HaveOccurred())
		drained := b.Drain()
		Expect(drained).To(HaveLen(1))
		Expect(drained[0]).To(Equal(t.UID))
	})

	It("does NOT trigger for an already-bound Ready tunnel", func() {
		ctx := context.Background()
		b := provisioning.NewBatcher[types.UID](500*time.Millisecond, 2*time.Second)
		c := &provisioning.PodController{Client: k8sClient, Batcher: b}

		port := int32(81)
		t := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "podctl-2"},
			Spec: v1alpha1.TunnelSpec{
				Ports: []v1alpha1.TunnelPort{{PublicPort: &port, ServicePort: 8080, Protocol: "TCP"}},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		t.Status.AssignedExit = "some-exit"
		t.Status.Phase = v1alpha1.TunnelPhaseReady
		Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())

		_, err := c.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "podctl-2"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(b.Drain()).To(BeEmpty())
	})
})

var _ = Describe("NodeController", func() {
	It("triggers the batcher when an ExitClaim event arrives", func() {
		ctx := context.Background()
		b := provisioning.NewBatcher[types.UID](500*time.Millisecond, 2*time.Second)
		c := &provisioning.NodeController{Client: k8sClient, Batcher: b}

		ec := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "nodectl-1"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, ec)).To(Succeed())

		_, err := c.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "nodectl-1"}})
		Expect(err).NotTo(HaveOccurred())
		drained := b.Drain()
		Expect(drained).To(HaveLen(1))
		Expect(drained[0]).To(Equal(ec.UID))
	})
})
