/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	goerrors "errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/admin"
	"github.com/mtaku3/frp-operator/internal/provider"
	"github.com/mtaku3/frp-operator/internal/provider/fake"
)

// fakeAdmin is an injectable AdminClient implementation. Tests toggle
// serverInfoOK between reconcile calls to drive Ready/Degraded transitions;
// because ServerInfo reads the field on every call (no captured snapshot),
// flips are picked up on the next reconcile.
type fakeAdmin struct {
	serverInfoOK bool
}

func (f *fakeAdmin) ServerInfo(_ context.Context) (*admin.ServerInfo, error) {
	if !f.serverInfoOK {
		return nil, goerrors.New("fake admin: not ready")
	}
	return &admin.ServerInfo{Version: "0.68.1"}, nil
}

func (f *fakeAdmin) PutConfigAndReload(_ context.Context, _ []byte) error {
	return nil
}

var _ = Describe("ExitServerController integration", func() {
	ctx := context.Background()

	var (
		fakeProv *fake.FakeProvisioner
		registry *provider.Registry
		recon    *ExitServerReconciler
		fa       *fakeAdmin
	)

	BeforeEach(func() {
		fakeProv = fake.New("digitalocean")
		registry = provider.NewRegistry()
		Expect(registry.Register(fakeProv)).To(Succeed())
		fa = &fakeAdmin{serverInfoOK: true}
		recon = &ExitServerReconciler{
			Client:         k8sClient,
			Scheme:         scheme.Scheme,
			Provisioners:   registry,
			NewAdminClient: func(_, _, _ string) AdminClient { return fa },
		}

		// The integration tests use CredentialsRef{Name: "x", Key: "y"};
		// the controller now loads it from a real Secret. Provide one.
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"},
			Data:       map[string][]byte{"y": []byte("test-do-token")},
		}
		if err := k8sClient.Create(ctx, sec); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(func() {
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"}}
			_ = k8sClient.Delete(ctx, s)
		})
	})

	It("provisions a fresh ExitServer and writes status", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-int", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		key := types.NamespacedName{Name: "exit-int", Namespace: "default"}
		req := ctrl.Request{NamespacedName: key}
		DeferCleanup(func() {
			got := &frpv1alpha1.ExitServer{}
			if err := k8sClient.Get(ctx, key, got); err == nil {
				_ = k8sClient.Delete(ctx, got)
				// Drive reconcile so finalizer is removed and CR cleared.
				Eventually(func() error {
					_, rerr := recon.Reconcile(ctx, req)
					if rerr != nil {
						return rerr
					}
					gerr := k8sClient.Get(ctx, key, got)
					if errors.IsNotFound(gerr) {
						return nil
					}
					if gerr != nil {
						return gerr
					}
					return goerrors.New("still present")
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			}
		})

		// First reconcile: adds finalizer.
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: ensures Secret + Provisioner.Create + status patch.
		_, err = recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Status now reflects the provisioned state.
		got := &frpv1alpha1.ExitServer{}
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.ProviderID).NotTo(BeEmpty())
		Expect(got.Status.PublicIP).To(Equal("127.0.0.1"))
		// Fake admin reports ServerInfo OK -> nextPhase yields Ready.
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))
		Expect(got.Status.LastReconcileTime).NotTo(BeNil())

		// Secret was created.
		var sec corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "exit-int-credentials", Namespace: "default"}, &sec)).To(Succeed())
		Expect(sec.Data["admin-password"]).NotTo(BeEmpty())
		Expect(sec.Data["auth-token"]).NotTo(BeEmpty())
	})

	It("destroys the underlying resource on CR delete", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-del", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "exit-del", Namespace: "default"}}
		// Drive provisioning so we have a ProviderID.
		for range 3 {
			_, err := recon.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		got := &frpv1alpha1.ExitServer{}
		Expect(k8sClient.Get(ctx, req.NamespacedName, got)).To(Succeed())
		providerID := got.Status.ProviderID
		Expect(providerID).NotTo(BeEmpty())

		// Delete the CR; reconcile should destroy and remove finalizer.
		Expect(k8sClient.Delete(ctx, got)).To(Succeed())
		Eventually(func() error {
			if _, err := recon.Reconcile(ctx, req); err != nil {
				return err
			}
			gerr := k8sClient.Get(ctx, req.NamespacedName, got)
			if errors.IsNotFound(gerr) {
				return nil
			}
			if gerr != nil {
				return gerr
			}
			return goerrors.New("still present")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Fake provisioner no longer holds the resource.
		_, inspectErr := fakeProv.Inspect(ctx, providerID)
		Expect(goerrors.Is(inspectErr, provider.ErrNotFound)).To(BeTrue())
	})

	It("transitions to Degraded when admin probe fails post-Ready", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-deg", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		key := types.NamespacedName{Name: "exit-deg", Namespace: "default"}
		req := ctrl.Request{NamespacedName: key}
		DeferCleanup(func() {
			got := &frpv1alpha1.ExitServer{}
			if err := k8sClient.Get(ctx, key, got); err == nil {
				_ = k8sClient.Delete(ctx, got)
				Eventually(func() error {
					_, rerr := recon.Reconcile(ctx, req)
					if rerr != nil {
						return rerr
					}
					gerr := k8sClient.Get(ctx, key, got)
					if errors.IsNotFound(gerr) {
						return nil
					}
					if gerr != nil {
						return gerr
					}
					return goerrors.New("still present")
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			}
		})

		// Drive reconciles to Ready with the fake admin reporting OK.
		for range 3 {
			_, err := recon.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		got := &frpv1alpha1.ExitServer{}
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))

		// Flip the fake admin to fail. Because ServerInfo reads the field
		// each call (not a captured snapshot), the next reconcile picks up
		// the failure and nextPhase transitions Ready -> Degraded.
		fa.serverInfoOK = false
		_, err := recon.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseDegraded))
	})
})
