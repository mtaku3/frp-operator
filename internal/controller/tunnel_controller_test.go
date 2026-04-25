/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
	"github.com/mtaku3/frp-operator/internal/provider/fake"
	"github.com/mtaku3/frp-operator/internal/scheduler"
)

var _ = Describe("TunnelController integration", func() {
	tctx := context.Background()

	var (
		fakeProv    *fake.FakeProvisioner
		provReg     *provider.Registry
		allocReg    *scheduler.AllocatorRegistry
		psReg       *scheduler.ProvisionStrategyRegistry
		exitRecon   *ExitServerReconciler
		tunnelRecon *TunnelReconciler
	)

	BeforeEach(func() {
		fakeProv = fake.New("digitalocean")
		provReg = provider.NewRegistry()
		Expect(provReg.Register(fakeProv)).To(Succeed())

		allocReg = scheduler.NewAllocatorRegistry()
		Expect(allocReg.Register(scheduler.CapacityAwareAllocator{})).To(Succeed())
		psReg = scheduler.NewProvisionStrategyRegistry()
		Expect(psReg.Register(scheduler.OnDemandStrategy{})).To(Succeed())

		fa := &fakeAdmin{serverInfoOK: true}
		exitRecon = &ExitServerReconciler{
			Client: k8sClient, Scheme: scheme.Scheme,
			Provisioners:   provReg,
			NewAdminClient: func(_, _, _ string) AdminClient { return fa },
		}
		tunnelRecon = &TunnelReconciler{
			Client: k8sClient, Scheme: scheme.Scheme,
			Allocators: allocReg, ProvisionStrategies: psReg,
			Provisioners:   provReg,
			NewAdminClient: func(_, _, _ string) AdminClient { return fa },
		}

		// SchedulingPolicy for these specs. Cluster-scoped; we use a
		// dedicated name (not "default") to avoid colliding with
		// crd_install_test which Creates a "default" policy fresh.
		// Idempotent across It blocks.
		desiredSpec := frpv1alpha1.SchedulingPolicySpec{
			VPS: frpv1alpha1.VPSSpec{
				Default: frpv1alpha1.VPSDefaults{
					Provider: frpv1alpha1.ProviderDigitalOcean,
					Regions:  []string{"nyc1"},
					Size:     "s-1vcpu-1gb",
				},
			},
		}
		var existing frpv1alpha1.SchedulingPolicy
		err := k8sClient.Get(tctx, types.NamespacedName{Name: "tunnel-test"}, &existing)
		if errors.IsNotFound(err) {
			Expect(k8sClient.Create(tctx, &frpv1alpha1.SchedulingPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "tunnel-test"},
				Spec:       desiredSpec,
			})).To(Succeed())
		} else {
			Expect(err).NotTo(HaveOccurred())
			existing.Spec.VPS = desiredSpec.VPS
			Expect(k8sClient.Update(tctx, &existing)).To(Succeed())
		}
	})

	It("provisions an exit when none exists, then schedules the tunnel onto it", func() {
		tunnel := &frpv1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
			Spec: frpv1alpha1.TunnelSpec{
				Service:             frpv1alpha1.ServiceRef{Name: "svc", Namespace: "default"},
				Ports:               []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80}},
				SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "tunnel-test"},
			},
		}
		Expect(k8sClient.Create(tctx, tunnel)).To(Succeed())
		tReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: "t1", Namespace: "default"}}

		DeferCleanup(func() {
			got := &frpv1alpha1.Tunnel{}
			if err := k8sClient.Get(tctx, tReq.NamespacedName, got); err == nil {
				_ = k8sClient.Delete(tctx, got)
				for i := 0; i < 5; i++ {
					_, _ = tunnelRecon.Reconcile(tctx, tReq)
				}
			}
			// Clean up any exit servers created in this spec.
			var exits frpv1alpha1.ExitServerList
			_ = k8sClient.List(tctx, &exits, client.InNamespace("default"))
			for i := range exits.Items {
				e := &exits.Items[i]
				_ = k8sClient.Delete(tctx, e)
				eReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: e.Name, Namespace: e.Namespace}}
				for j := 0; j < 5; j++ {
					_, _ = exitRecon.Reconcile(tctx, eReq)
				}
			}
		})

		// First reconcile: adds finalizer.
		_, err := tunnelRecon.Reconcile(tctx, tReq)
		Expect(err).NotTo(HaveOccurred())

		// Second: invokes scheduler, no exit -> ProvisionStrategy creates one.
		_, err = tunnelRecon.Reconcile(tctx, tReq)
		Expect(err).NotTo(HaveOccurred())

		// Drive ExitServerController to ready the new exit.
		var exits frpv1alpha1.ExitServerList
		Expect(k8sClient.List(tctx, &exits, client.InNamespace("default"))).To(Succeed())
		Expect(exits.Items).To(HaveLen(1))
		exitName := exits.Items[0].Name
		eReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: exitName, Namespace: "default"}}
		for i := 0; i < 3; i++ {
			_, err := exitRecon.Reconcile(tctx, eReq)
			Expect(err).NotTo(HaveOccurred())
		}
		var ex frpv1alpha1.ExitServer
		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
		Expect(ex.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))

		// Final reconcile loops on the tunnel: schedule + frpc.
		for i := 0; i < 3; i++ {
			_, err := tunnelRecon.Reconcile(tctx, tReq)
			Expect(err).NotTo(HaveOccurred())
		}
		var got frpv1alpha1.Tunnel
		Expect(k8sClient.Get(tctx, tReq.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.AssignedExit).To(Equal(exitName))
		Expect(got.Status.AssignedPorts).To(Equal([]int32{80}))
		Expect(got.Status.AssignedIP).To(Equal("127.0.0.1"))
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.TunnelConnecting))

		var sec corev1.Secret
		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: "t1-frpc-config", Namespace: "default"}, &sec)).To(Succeed())
		var dep appsv1.Deployment
		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: "t1-frpc", Namespace: "default"}, &dep)).To(Succeed())
		Expect(*dep.Spec.Replicas).To(Equal(int32(1)))

		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
		Expect(ex.Status.Allocations["80"]).To(Equal("default/t1"))
	})

	It("releases ports on tunnel deletion", func() {
		tunnel := &frpv1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "t2", Namespace: "default"},
			Spec: frpv1alpha1.TunnelSpec{
				Service:             frpv1alpha1.ServiceRef{Name: "svc2", Namespace: "default"},
				Ports:               []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 81}},
				SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "tunnel-test"},
			},
		}
		Expect(k8sClient.Create(tctx, tunnel)).To(Succeed())
		tReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: "t2", Namespace: "default"}}

		var exitName string
		DeferCleanup(func() {
			got := &frpv1alpha1.Tunnel{}
			if err := k8sClient.Get(tctx, tReq.NamespacedName, got); err == nil {
				_ = k8sClient.Delete(tctx, got)
				for i := 0; i < 5; i++ {
					_, _ = tunnelRecon.Reconcile(tctx, tReq)
				}
			}
			var exits frpv1alpha1.ExitServerList
			_ = k8sClient.List(tctx, &exits, client.InNamespace("default"))
			for i := range exits.Items {
				e := &exits.Items[i]
				_ = k8sClient.Delete(tctx, e)
				eReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: e.Name, Namespace: e.Namespace}}
				for j := 0; j < 5; j++ {
					_, _ = exitRecon.Reconcile(tctx, eReq)
				}
			}
		})

		// Drive tunnel until it has an assigned exit (which goes through
		// "create exit -> reconcile exit to Ready -> schedule tunnel").
		// Reconcile #1: finalizer.
		_, err := tunnelRecon.Reconcile(tctx, tReq)
		Expect(err).NotTo(HaveOccurred())
		// Reconcile #2: provision exit.
		_, err = tunnelRecon.Reconcile(tctx, tReq)
		Expect(err).NotTo(HaveOccurred())

		var exits frpv1alpha1.ExitServerList
		Expect(k8sClient.List(tctx, &exits, client.InNamespace("default"))).To(Succeed())
		Expect(exits.Items).To(HaveLen(1))
		exitName = exits.Items[0].Name
		eReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: exitName, Namespace: "default"}}
		for i := 0; i < 3; i++ {
			_, err := exitRecon.Reconcile(tctx, eReq)
			Expect(err).NotTo(HaveOccurred())
		}
		// Tunnel reconciles to placement.
		for i := 0; i < 3; i++ {
			_, err := tunnelRecon.Reconcile(tctx, tReq)
			Expect(err).NotTo(HaveOccurred())
		}
		var ex frpv1alpha1.ExitServer
		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
		Expect(ex.Status.Allocations["81"]).To(Equal("default/t2"))

		// Delete the tunnel.
		var got frpv1alpha1.Tunnel
		Expect(k8sClient.Get(tctx, tReq.NamespacedName, &got)).To(Succeed())
		Expect(k8sClient.Delete(tctx, &got)).To(Succeed())

		// Drive reconciles until the tunnel is gone (finalizer removed).
		Eventually(func() bool {
			_, _ = tunnelRecon.Reconcile(tctx, tReq)
			err := k8sClient.Get(tctx, tReq.NamespacedName, &frpv1alpha1.Tunnel{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())

		// Allocations should be cleared on the exit.
		Expect(k8sClient.Get(tctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
		_, present := ex.Status.Allocations["81"]
		Expect(present).To(BeFalse())
	})
})
