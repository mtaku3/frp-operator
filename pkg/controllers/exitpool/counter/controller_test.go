package counter_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("counter controller", func() {
	const poolName = "pool-c"

	makePool := func() *v1alpha1.ExitPool {
		return &v1alpha1.ExitPool{
			ObjectMeta: metav1.ObjectMeta{Name: poolName},
			Spec: v1alpha1.ExitPoolSpec{
				Template: v1alpha1.ExitClaimTemplate{
					Spec: v1alpha1.ExitClaimTemplateSpec{
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
				},
			},
		}
	}

	makeClaim := func(name string) *v1alpha1.ExitClaim {
		return &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{v1alpha1.LabelExitPool: poolName},
			},
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

	setAllocatable := func(name string, alloc corev1.ResourceList) {
		ctx := context.Background()
		var c v1alpha1.ExitClaim
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &c)).To(Succeed())
		c.Status.Allocatable = alloc
		Expect(k8sClient.Status().Update(ctx, &c)).To(Succeed())
	}

	It("rolls up child claim allocatable into Pool.Status.Resources", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool())).To(Succeed())

		for i := range 3 {
			n := fmt.Sprintf("c-%d", i)
			Expect(k8sClient.Create(ctx, makeClaim(n))).To(Succeed())
			setAllocatable(n, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			})
		}

		Eventually(func(g Gomega) {
			var got v1alpha1.ExitPool
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &got)).To(Succeed())
			g.Expect(got.Status.Exits).To(Equal(int64(3)))
			cpu := got.Status.Resources[corev1.ResourceCPU]
			mem := got.Status.Resources[corev1.ResourceMemory]
			exits := got.Status.Resources[corev1.ResourceName(v1alpha1.ResourceExits)]
			g.Expect(cpu.Cmp(resource.MustParse("6"))).To(Equal(0))
			g.Expect(mem.Cmp(resource.MustParse("3Gi"))).To(Equal(0))
			g.Expect(exits.Cmp(resource.MustParse("3"))).To(Equal(0))
		}, 15*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("decrements Status.Exits when a claim is deleted", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool())).To(Succeed())

		for i := range 2 {
			n := fmt.Sprintf("d-%d", i)
			Expect(k8sClient.Create(ctx, makeClaim(n))).To(Succeed())
			setAllocatable(n, corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")})
		}

		Eventually(func(g Gomega) {
			var got v1alpha1.ExitPool
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &got)).To(Succeed())
			g.Expect(got.Status.Exits).To(Equal(int64(2)))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		var c v1alpha1.ExitClaim
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "d-0"}, &c)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &c)).To(Succeed())

		Eventually(func(g Gomega) {
			var got v1alpha1.ExitPool
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &got)).To(Succeed())
			g.Expect(got.Status.Exits).To(Equal(int64(1)))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
