package controller

import (
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// reservePorts atomically writes the requested ports into
// exit.Status.Allocations under tunnelKey ("namespace/name") and
// increments Usage.Tunnels by 1. Fails if any port is already allocated
// to a different tunnel. Uses the apiserver's resourceVersion as the
// concurrency token: a Patch conflict surfaces as a Conflict error from
// the client, which the caller should treat as a requeue signal.
//
// The exit pointer is mutated in place to reflect the patched state so
// callers can read .Allocations after a successful return.
func reservePorts(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, ports []int32, tunnelKey string) error {
	if exit.Status.Allocations == nil {
		exit.Status.Allocations = map[string]string{}
	}
	patch := client.MergeFrom(exit.DeepCopy())

	// Check first: every port must be free (or already allocated to *us*).
	for _, p := range ports {
		key := strconv.Itoa(int(p))
		if owner, taken := exit.Status.Allocations[key]; taken && owner != tunnelKey {
			return fmt.Errorf("port %d already allocated to %s", p, owner)
		}
	}

	// Reserve.
	added := 0
	for _, p := range ports {
		key := strconv.Itoa(int(p))
		if _, already := exit.Status.Allocations[key]; !already {
			added++
		}
		exit.Status.Allocations[key] = tunnelKey
	}
	// Tunnel count increments only when this tunnel didn't already
	// hold any of these ports (added > 0 implies new presence).
	if added > 0 {
		exit.Status.Usage.Tunnels++
	}
	return c.Status().Patch(ctx, exit, patch)
}

// releasePorts removes every Allocation entry pointing at tunnelKey and
// decrements Usage.Tunnels by 1 if any were removed. Idempotent: calling
// when the tunnel holds no allocations is a no-op.
func releasePorts(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, tunnelKey string) error {
	if len(exit.Status.Allocations) == 0 {
		return nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	removed := 0
	for k, owner := range exit.Status.Allocations {
		if owner == tunnelKey {
			delete(exit.Status.Allocations, k)
			removed++
		}
	}
	if removed == 0 {
		return nil
	}
	if exit.Status.Usage.Tunnels > 0 {
		exit.Status.Usage.Tunnels--
	}
	return c.Status().Patch(ctx, exit, patch)
}
