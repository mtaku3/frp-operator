/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package operator provides helpers to wait for the frp-operator
// Deployment + a CRD-aware admission probe. With webhooks gone, the
// readiness signal is just (a) Deployment.ReadyReplicas matches Spec
// AND (b) a dry-run create of a tiny ExitPool is accepted by the
// apiserver. The latter proves CRDs are installed and the apiserver
// recognizes the schema; nothing operator-side admits ExitPools (no
// webhook + no CEL on root spec), so the dry-run succeeds even if the
// operator pod is still booting — but combined with the Deployment
// ready check, it is a sufficient signal that the cluster is ready
// for tests.
package operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const (
	// Namespace is where the operator runs.
	Namespace = "frp-operator-system"
	// DeploymentName is the operator's Deployment after kustomize namePrefix.
	DeploymentName = "frp-operator-controller-manager"
)

// IsReady returns (true, nil) when both signals pass.
func IsReady(ctx context.Context, c client.Client) (bool, error) {
	if ok, err := isDeploymentReady(ctx, c); err != nil || !ok {
		return false, err
	}
	return isCRDReady(ctx, c)
}

// WaitForOperatorReady polls IsReady until the timeout elapses.
func WaitForOperatorReady(ctx context.Context, c client.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ready, err := IsReady(ctx, c)
		if err == nil && ready {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("operator not Ready within %s: %w", timeout, lastErr)
		}
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

// isCRDReady dry-run-creates a tiny ExitPool. Success means the CRD is
// installed and the apiserver accepts the schema. The dry-run is
// rolled back atomically — no object is created.
func isCRDReady(ctx context.Context, c client.Client) (bool, error) {
	probe := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "readiness-probe-"},
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
		},
	}
	err := c.Create(ctx, probe, &client.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
		return false, nil
	}
	// Other errors (e.g. transient network) — retry without surfacing.
	return false, nil
}
