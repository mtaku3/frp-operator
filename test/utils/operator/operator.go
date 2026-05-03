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

package operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Namespace is the namespace the operator runs in.
const Namespace = "frp-operator-system"

// DeploymentName is the operator's Deployment name (after kustomize
// namePrefix rewriting).
const DeploymentName = "frp-operator-controller-manager"

// IsReady checks the operator Deployment, the webhook cert injection,
// and (if checkWebhook is true) does a dry-run admission probe to
// verify the webhook is actually live.
func IsReady(ctx context.Context, c client.Client, checkWebhook bool) (bool, error) {
	if ok, err := isDeploymentReady(ctx, c); err != nil || !ok {
		return false, err
	}
	if !checkWebhook {
		return true, nil
	}
	if err := checkWebhookSetup(ctx, c); err != nil {
		return false, err
	}
	return isWebhookWorking(ctx, c)
}

// WaitForReady polls IsReady until it returns true or the timeout
// elapses.
func WaitForReady(ctx context.Context, c client.Client, timeout time.Duration, checkWebhook bool) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("operator did not become Ready within %s: %w", timeout, lastErr)
		}
		ready, err := IsReady(ctx, c, checkWebhook)
		if err == nil && ready {
			return nil
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
}

func isDeploymentReady(ctx context.Context, c client.Client) (bool, error) {
	var d appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: DeploymentName}, &d); err != nil {
		return false, err
	}
	if d.Spec.Replicas == nil {
		return false, nil
	}
	return d.Status.ReadyReplicas >= *d.Spec.Replicas, nil
}
