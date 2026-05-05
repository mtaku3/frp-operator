package validation_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("validation controller", func() {
	makePool := func(name string, replicas *int64, weight *int32) *v1alpha1.ExitPool {
		return &v1alpha1.ExitPool{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: v1alpha1.ExitPoolSpec{
				Replicas: replicas,
				Weight:   weight,
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

	getPool := func(name string) *v1alpha1.ExitPool {
		var p v1alpha1.ExitPool
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, &p)).To(Succeed())
		return &p
	}

	int64Ptr := func(v int64) *int64 { return &v }
	int32Ptr := func(v int32) *int32 { return &v }

	It("flags both replicas and weight set as ValidationSucceeded=False", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool("pool-bad", int64Ptr(3), int32Ptr(50)))).To(Succeed())
		Eventually(func(g Gomega) {
			p := getPool("pool-bad")
			c := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeValidationSucceeded)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(c.Reason).To(Equal(v1alpha1.ReasonInvalidRequirements))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("passes when only weight is set", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool("pool-weight", nil, int32Ptr(50)))).To(Succeed())
		Eventually(func(g Gomega) {
			p := getPool("pool-weight")
			c := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeValidationSucceeded)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("passes when neither replicas nor weight is set", func() {
		ctx := context.Background()
		Expect(k8sClient.Create(ctx, makePool("pool-empty", nil, nil))).To(Succeed())
		Eventually(func(g Gomega) {
			p := getPool("pool-empty")
			c := apimeta.FindStatusCondition(p.Status.Conditions, v1alpha1.ConditionTypeValidationSucceeded)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
