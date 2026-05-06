package hash_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/hash"
)

var _ = Describe("hash controller", func() {
	const poolName = "pool-h"

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

	It("stamps the hash on the pool and on every child claim", func() {
		ctx := context.Background()
		pool := makePool()
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeClaim("c-1"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeClaim("c-2"))).To(Succeed())

		expected, err := hash.PoolTemplateHash(pool)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			var got v1alpha1.ExitPool
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &got)).To(Succeed())
			g.Expect(got.Annotations).To(HaveKeyWithValue(v1alpha1.AnnotationPoolHash, expected))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		Eventually(func(g Gomega) {
			for _, n := range []string{"c-1", "c-2"} {
				var c v1alpha1.ExitClaim
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: n}, &c)).To(Succeed())
				g.Expect(c.Annotations).To(HaveKeyWithValue(v1alpha1.AnnotationPoolHash, expected))
			}
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("updates the children when the template changes", func() {
		ctx := context.Background()
		pool := makePool()
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeClaim("c-3"))).To(Succeed())

		oldHash, _ := hash.PoolTemplateHash(pool)
		Eventually(func(g Gomega) {
			var c v1alpha1.ExitClaim
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "c-3"}, &c)).To(Succeed())
			g.Expect(c.Annotations[v1alpha1.AnnotationPoolHash]).To(Equal(oldHash))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		// Mutate the template — version bump.
		Eventually(func() error {
			var p v1alpha1.ExitPool
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &p); err != nil {
				return err
			}
			p.Spec.Template.Spec.Frps.Version = "v0.69.0"
			return k8sClient.Update(ctx, &p)
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		var updated v1alpha1.ExitPool
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolName}, &updated)).To(Succeed())
		newHash, _ := hash.PoolTemplateHash(&updated)
		Expect(newHash).NotTo(Equal(oldHash))

		Eventually(func(g Gomega) {
			var c v1alpha1.ExitClaim
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "c-3"}, &c)).To(Succeed())
			g.Expect(c.Annotations[v1alpha1.AnnotationPoolHash]).To(Equal(newHash))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
