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
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/pkg/utils"
)

// SetAsOwnedByOperatorDeployment sets the controlled object as owned by the operator deployment.
//
// IMPORTANT: The controlled resource must reside in the same namespace as the operator as described by:
// https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
func SetAsOwnedByOperatorDeployment(ctx context.Context,
	kubeClient client.Client,
	controlled *metav1.ObjectMeta,
	operatorLabelSelector string,
) error {
	deployment, err := GetOperatorDeployment(ctx, kubeClient, controlled.Namespace, operatorLabelSelector)
	if err != nil {
		return err
	}

	// The deployment typeMeta is empty (kubernetes bug), so we need to explicitly populate it.
	typeMeta := metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	utils.SetAsOwnedBy(controlled, deployment.ObjectMeta, typeMeta)

	return nil
}

// GetOperatorDeployment find the operator deployment using labels
// and then return the deployment object, in case we can't find a deployment
// or we find more than one, we just return an error.
func GetOperatorDeployment(
	ctx context.Context,
	kubeClient client.Client,
	namespace, operatorLabelSelector string,
) (*appsv1.Deployment, error) {
	labelMap, err := labels.ConvertSelectorToLabelsMap(operatorLabelSelector)
	if err != nil {
		return nil, err
	}
	deployment, err := findOperatorDeploymentByFilter(ctx,
		kubeClient,
		namespace,
		client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
	if err != nil {
		return nil, err
	}
	if deployment != nil {
		return deployment, nil
	}

	deployment, err = findOperatorDeploymentByFilter(ctx,
		kubeClient,
		namespace,
		client.HasLabels{"operators.coreos.com/cloudnative-pg.openshift-operators="})
	if err != nil {
		return nil, err
	}
	if deployment != nil {
		return deployment, nil
	}

	return nil, fmt.Errorf("no deployment detected")
}

// findOperatorDeploymentByFilter search in a defined namespace
// looking for a deployment with the defined filter
func findOperatorDeploymentByFilter(ctx context.Context,
	kubeClient client.Client,
	namespace string,
	filter client.ListOption,
) (*appsv1.Deployment, error) {
	logger := log.FromContext(ctx)

	deploymentList := &appsv1.DeploymentList{}
	err := kubeClient.List(
		ctx,
		deploymentList,
		client.InNamespace(namespace),
		filter,
	)
	if err != nil {
		return nil, err
	}
	switch {
	case len(deploymentList.Items) == 1:
		return &deploymentList.Items[0], nil
	case len(deploymentList.Items) > 1:
		err = fmt.Errorf("more than one operator deployment running")
		logger.Error(err, "more than one operator deployment found with the filter", "filter", filter)
		return nil, err
	}

	err = fmt.Errorf("no operator deployment found")
	logger.Error(err, "no operator deployment found with the filter", "filter", filter)
	return nil, err
}
