//go:build e2e

/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("CEL/OpenAPI validation", func() {
	It("rejects ExitPool with weight > 100", func() {
		w := int32(200)
		pool := &v1alpha1.ExitPool{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-weight"},
			Spec: v1alpha1.ExitPoolSpec{
				Template: v1alpha1.ExitClaimTemplate{
					Spec: v1alpha1.ExitClaimTemplateSpec{
						ProviderClassRef: v1alpha1.ProviderClassRef{
							Group: v1alpha1.Group,
							Kind:  "LocalDockerProviderClass",
							Name:  "default",
						},
						Frps: v1alpha1.FrpsConfig{
							Version:    "v0.68.1",
							AllowPorts: []string{"80"},
							Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
						},
					},
				},
				Weight: &w,
			},
		}

		err := k8sClient.Create(suiteCtx, pool)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.weight"))
		Expect(err.Error()).To(ContainSubstring("less than or equal to 100"))
	})
})
