package readiness_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

var _ = Describe("readiness controller", func() {
	makePool := func(name, kind, className string) *v1alpha1.ExitPool {
		return &v1alpha1.ExitPool{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: v1alpha1.ExitPoolSpec{
				Template: v1alpha1.ExitClaimTemplate{
					Spec: v1alpha1.ExitClaimTemplateSpec{
						ProviderClassRef: v1alpha1.ProviderClassRef{
							Group: "frp.operator.io",
							Kind:  kind,
							Name:  className,
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

	getPool := func(name string) *v1alpha1.ExitPool {
		var p v1alpha1.ExitPool
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, &p)).To(Succeed())
		return &p
	}

	It("sets ProviderClassReady=False when the kind is not registered", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool("pool-unregistered", "BogusKind", "default"))).To(Succeed())
		Eventually(func(g Gomega) {
			p := getPool("pool-unregistered")
			c := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeProviderClassReady)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(c.Reason).To(Equal(v1alpha1.ReasonProviderClassNotFound))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("sets ProviderClassReady=False when the referenced object is missing", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool("pool-missing", "LocalDockerProviderClass", "ghost"))).To(Succeed())
		Eventually(func(g Gomega) {
			p := getPool("pool-missing")
			c := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeProviderClassReady)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(c.Reason).To(Equal(v1alpha1.ReasonProviderClassNotFound))
			ready := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("flips both conditions to True when the class exists", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, &ldv1alpha1.LocalDockerProviderClass{
			ObjectMeta: metav1.ObjectMeta{Name: "real"},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, makePool("pool-ok", "LocalDockerProviderClass", "real"))).To(Succeed())

		Eventually(func(g Gomega) {
			p := getPool("pool-ok")
			pcr := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeProviderClassReady)
			g.Expect(pcr).NotTo(BeNil())
			g.Expect(pcr.Status).To(Equal(metav1.ConditionTrue))
			ready := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
