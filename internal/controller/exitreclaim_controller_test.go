package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("ExitReclaimController", func() {
	ctx := context.Background()

	var (
		recon  *ExitReclaimReconciler
		clock  *time.Time
		policy *frpv1alpha1.SchedulingPolicy
	)

	BeforeEach(func() {
		t := time.Now()
		clock = &t
		recon = &ExitReclaimReconciler{
			Client:     k8sClient,
			Scheme:     scheme.Scheme,
			PolicyName: "reclaim-test",
			Now:        func() time.Time { return *clock },
		}
		policy = &frpv1alpha1.SchedulingPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "reclaim-test"},
			Spec: frpv1alpha1.SchedulingPolicySpec{
				Consolidation: frpv1alpha1.ConsolidationSpec{
					ReclaimEmpty: true,
					DrainAfter:   metav1.Duration{Duration: 100 * time.Millisecond},
				},
			},
		}
		// Idempotent create.
		if err := k8sClient.Create(ctx, policy); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				Fail("create policy: " + err.Error())
			}
			// Reset spec on re-use.
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "reclaim-test"}, policy)).To(Succeed())
			policy.Spec = frpv1alpha1.SchedulingPolicySpec{
				Consolidation: frpv1alpha1.ConsolidationSpec{
					ReclaimEmpty: true,
					DrainAfter:   metav1.Duration{Duration: 100 * time.Millisecond},
				},
			}
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())
		}
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, policy) })
	})

	It("starts drain on a Ready empty exit and destroys after drainAfter", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "rclm-1", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, exit) })

		// Mark Ready with no allocations via a status update.
		exit.Status.Phase = frpv1alpha1.PhaseReady
		Expect(k8sClient.Status().Update(ctx, exit)).To(Succeed())

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "rclm-1", Namespace: "default"}}

		// First reconcile: starts drain.
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		var got frpv1alpha1.ExitServer
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseDraining))
		Expect(got.Status.DrainStartedAt).NotTo(BeNil())

		// Advance clock past drainAfter.
		future := clock.Add(200 * time.Millisecond)
		*clock = future

		// Second reconcile: destroy.
		_, err = recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Exit should be gone (or marked for deletion).
		err = k8sClient.Get(ctx, req.NamespacedName, &got)
		Expect(apierrors.IsNotFound(err) || got.DeletionTimestamp != nil).To(BeTrue())
	})

	It("aborts drain when a tunnel allocates to the draining exit", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "rclm-2", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, exit) })

		// Start in Draining state with no allocations.
		ts := metav1.Now()
		exit.Status.Phase = frpv1alpha1.PhaseDraining
		exit.Status.DrainStartedAt = &ts
		Expect(k8sClient.Status().Update(ctx, exit)).To(Succeed())

		// Now add an allocation (simulating a hard-pinned tunnel).
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "rclm-2", Namespace: "default"}, exit)).To(Succeed())
		exit.Status.Allocations = map[string]string{"443": "ns/foo"}
		Expect(k8sClient.Status().Update(ctx, exit)).To(Succeed())

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "rclm-2", Namespace: "default"}}
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got frpv1alpha1.ExitServer
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))
		Expect(got.Status.DrainStartedAt).To(BeNil())
	})

	It("respects per-exit reclaim opt-out annotation", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rclm-3",
				Namespace: "default",
				Annotations: map[string]string{
					"frp-operator.io/reclaim": "false",
				},
			},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, exit) })

		exit.Status.Phase = frpv1alpha1.PhaseReady
		Expect(k8sClient.Status().Update(ctx, exit)).To(Succeed())

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "rclm-3", Namespace: "default"}}
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got frpv1alpha1.ExitServer
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady), "annotation should prevent drain")
		Expect(got.Status.DrainStartedAt).To(BeNil())
	})
})
