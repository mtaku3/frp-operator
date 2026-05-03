/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/scheduler"
)

// resolvePolicy fetches the SchedulingPolicy named by tunnel.spec.schedulingPolicyRef,
// falling back to "default" if no name is set.
func resolvePolicy(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (*frpv1alpha1.SchedulingPolicy, error) {
	name := t.Spec.SchedulingPolicyRef.Name
	if name == "" {
		name = "default"
	}
	var p frpv1alpha1.SchedulingPolicy
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &p); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("SchedulingPolicy %q not found", name)
		}
		return nil, fmt.Errorf("get SchedulingPolicy: %w", err)
	}
	return &p, nil
}

// listExitsInScope returns ExitServers in the tunnel's namespace. The
// reader passed in should be an apiserver-direct reader (not the
// informer cache) so the scheduler can't miss an exit that has just
// reached Phase=Ready: cache lag here would cause the scheduler to
// provision a fresh exit instead of binpacking onto the existing one.
func listExitsInScope(ctx context.Context, c client.Reader, t *frpv1alpha1.Tunnel) ([]frpv1alpha1.ExitServer, error) {
	var list frpv1alpha1.ExitServerList
	if err := c.List(ctx, &list, client.InNamespace(t.Namespace)); err != nil {
		return nil, fmt.Errorf("list exits: %w", err)
	}
	return list.Items, nil
}

// createExitServerFromDecision creates a new ExitServer CR using the
// ProvisionDecision's Spec, backfilling required fields that the pure
// scheduler doesn't compose (CredentialsRef, Frps.Version, AllowPorts).
func createExitServerFromDecision(
	ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel, d scheduler.ProvisionDecision,
) (*frpv1alpha1.ExitServer, error) {
	spec := d.Spec
	if spec.Frps.Version == "" {
		spec.Frps.Version = "v0.68.1"
	}
	if len(spec.AllowPorts) == 0 {
		// Defensive fallback; OnDemandStrategy.composeSpec already populates
		// this from the tunnel's requested ports.
		spec.AllowPorts = []string{"1024-65535"}
	}
	if spec.CredentialsRef.Name == "" {
		spec.CredentialsRef = frpv1alpha1.SecretKeyRef{
			Name: string(spec.Provider) + "-credentials",
			Key:  "token",
		}
	}
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: t.Name + "-",
			Namespace:    t.Namespace,
			Labels:       map[string]string{"frp-operator.io/created-by": "tunnel-controller"},
		},
		Spec: spec,
	}
	if err := c.Create(ctx, exit); err != nil {
		return nil, fmt.Errorf("create ExitServer: %w", err)
	}
	return exit, nil
}
