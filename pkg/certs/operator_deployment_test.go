/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

package certs

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createFakeOperatorDeploymentByName(ctx context.Context,
	kubeClient client.Client,
	deploymentName string,
	labels map[string]string,
) error {
	operatorDep := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: operatorNamespaceName,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{},
	}

	return kubeClient.Create(ctx, &operatorDep)
}

func deleteFakeOperatorDeployment(ctx context.Context,
	kubeClient client.Client,
	deploymentName string,
) error {
	operatorDep := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: operatorNamespaceName,
		},
		Spec: appsv1.DeploymentSpec{},
	}

	return kubeClient.Delete(ctx, &operatorDep)
}

var _ = Describe("Difference of values of maps", func() {
	It("will always set the app.kubernetes.io/name to cloudnative-pg", func(ctx SpecContext) {
		operatorLabelSelector := "app.kubernetes.io/name=cloudnative-pg"
		operatorLabels := map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		kubeClient := generateFakeClient()
		err := createFakeOperatorDeploymentByName(ctx, kubeClient, operatorDeploymentName, operatorLabels)
		Expect(err).ToNot(HaveOccurred())
		labelMap, err := labels.ConvertSelectorToLabelsMap(operatorLabelSelector)
		Expect(err).ToNot(HaveOccurred())

		deployment, err := findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).ToNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())

		err = deleteFakeOperatorDeployment(ctx, kubeClient, operatorDeploymentName)
		Expect(err).ToNot(HaveOccurred())

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "some-app",
		}
		err = createFakeOperatorDeploymentByName(ctx, kubeClient, "some-app", operatorLabels)
		Expect(err).ToNot(HaveOccurred())
		deployment, err = findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).To(HaveOccurred())
		Expect(deployment).To(BeNil())

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		err = createFakeOperatorDeploymentByName(ctx, kubeClient, operatorNamespaceName, operatorLabels)
		Expect(err).ToNot(HaveOccurred())
		deployment, err = findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).ToNot(HaveOccurred())
		Expect(deployment).ToNot(BeNil())
	})
})
