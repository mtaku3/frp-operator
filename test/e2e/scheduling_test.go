//go:build e2e

/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	exitclaimutil "github.com/mtaku3/frp-operator/test/utils/exitclaim"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	tunnelutil "github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Scheduling", Ordered, func() {
	const ns = "default"

	Describe("binpack two tunnels onto one ExitClaim", Ordered, func() {
		BeforeAll(func() {
			yaml, err := os.ReadFile(filepath.Join("fixtures", "scheduling.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(kubernetes.ApplyServerSide(suiteCtx, yaml)).To(Succeed())
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "-f",
				filepath.Join("fixtures", "scheduling.yaml"),
				"--ignore-not-found", "--wait=false"))
		})

		It("places sched-a and sched-b on the same ExitClaim", func() {
			Eventually(func(g Gomega) {
				a, err := tunnelutil.Get(suiteCtx, k8sClient, ns, "sched-a")
				g.Expect(err).NotTo(HaveOccurred())
				b, err := tunnelutil.Get(suiteCtx, k8sClient, ns, "sched-b")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(a.Status.Phase).To(Equal(v1alpha1.TunnelPhaseReady))
				g.Expect(b.Status.Phase).To(Equal(v1alpha1.TunnelPhaseReady))
				g.Expect(a.Status.AssignedExit).NotTo(BeEmpty())
				g.Expect(a.Status.AssignedExit).To(Equal(b.Status.AssignedExit))
			}, 4*time.Minute, 5*time.Second).Should(Succeed())
		})
	})

	Describe("scheduler refusal when port is excluded by AllowPorts", Ordered, func() {
		const refusalSvc = "refusal-svc"
		var (
			origAllowPorts []string
			pool           v1alpha1.ExitPool
		)

		BeforeAll(func() {
			Expect(k8sClient.Get(suiteCtx, types.NamespacedName{Name: "default"}, &pool)).To(Succeed())
			origAllowPorts = pool.Spec.Template.Spec.Frps.AllowPorts
			updated := pool.DeepCopy()
			updated.Spec.Template.Spec.Frps.AllowPorts = []string{"80", "81", "1024-65535"}
			Expect(k8sClient.Update(suiteCtx, updated)).To(Succeed())

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: refusalSvc, Namespace: ns},
				Spec: corev1.ServiceSpec{
					Type:              corev1.ServiceTypeLoadBalancer,
					LoadBalancerClass: ptrString("frp.operator.io/frp"),
					Selector:          map[string]string{"app": "nope"},
					Ports: []corev1.ServicePort{{
						Name:       "ssh",
						Port:       22,
						TargetPort: intstr.FromInt(22),
						Protocol:   corev1.ProtocolTCP,
					}},
				},
			}
			Expect(k8sClient.Create(suiteCtx, svc)).To(Succeed())
		})

		AfterAll(func() {
			_ = k8sClient.Delete(suiteCtx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: refusalSvc, Namespace: ns},
			})
			var current v1alpha1.ExitPool
			if err := k8sClient.Get(suiteCtx, types.NamespacedName{Name: "default"}, &current); err == nil {
				current.Spec.Template.Spec.Frps.AllowPorts = origAllowPorts
				_ = k8sClient.Update(suiteCtx, &current)
			}
		})

		It("does not allocate a new ExitClaim and leaves the tunnel Allocating", func() {
			before, err := exitclaimutil.ListForPool(suiteCtx, k8sClient, "default")
			Expect(err).NotTo(HaveOccurred())

			Consistently(func(g Gomega) {
				t, err := tunnelutil.Get(suiteCtx, k8sClient, ns, refusalSvc)
				if apierrors.IsNotFound(err) {
					return
				}
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(t.Status.Phase).NotTo(Equal(v1alpha1.TunnelPhaseReady))
				after, err := exitclaimutil.ListForPool(suiteCtx, k8sClient, "default")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(after)).To(BeNumerically("<=", len(before)))
			}, 60*time.Second, 5*time.Second).Should(Succeed())
		})
	})
})

func ptrString(s string) *string { return &s }
