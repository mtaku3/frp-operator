package state

import (
	"context"
	"fmt"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Synced performs a bidirectional reconciliation: cache and apiserver
// must agree on the existing object set.
//
// It checks both directions:
//
//  1. API has X but cache doesn't (informer lag — newly-created object
//     hasn't been delivered yet).
//  2. Cache has X but API doesn't (stale tombstone — a delete event
//     was missed).
//
// Returns true iff the cluster cache is consistent with apiserver state
// at the moment of the call. False signals callers to retry — the
// informer is lagging or out-of-sync.
//
// Decision-making controllers MUST call this and bail-with-requeue if
// it returns false; otherwise they may bin-pack onto a non-existent
// exit (cache stale) or fail to see a freshly-Ready exit (cache lag).
func (c *Cluster) Synced(ctx context.Context) bool {
	var claims v1alpha1.ExitClaimList
	if err := c.kubeClient.List(ctx, &claims); err != nil {
		return false
	}
	var pools v1alpha1.ExitPoolList
	if err := c.kubeClient.List(ctx, &pools); err != nil {
		return false
	}
	var tunnels v1alpha1.TunnelList
	if err := c.kubeClient.List(ctx, &tunnels); err != nil {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build "what API has" sets for the inverse check.
	apiClaimNames := make(map[string]struct{}, len(claims.Items))
	apiClaimProviderIDs := make(map[string]struct{}, len(claims.Items))
	for i := range claims.Items {
		claim := &claims.Items[i]
		apiClaimNames[claim.Name] = struct{}{}
		if claim.Status.ProviderID != "" {
			apiClaimProviderIDs[claim.Status.ProviderID] = struct{}{}
		}
		if claim.Status.ProviderID == "" {
			continue
		}
		if _, ok := c.exits[claim.Status.ProviderID]; !ok {
			return false
		}
	}
	apiPoolNames := make(map[string]struct{}, len(pools.Items))
	for i := range pools.Items {
		apiPoolNames[pools.Items[i].Name] = struct{}{}
		if _, ok := c.pools[pools.Items[i].Name]; !ok {
			return false
		}
	}
	apiTunnelKeys := make(map[TunnelKey]string, len(tunnels.Items))
	for i := range tunnels.Items {
		t := &tunnels.Items[i]
		key := TunnelKey(fmt.Sprintf("%s/%s", t.Namespace, t.Name))
		apiTunnelKeys[key] = t.Status.AssignedExit
		if t.Status.AssignedExit == "" {
			continue
		}
		if b, ok := c.bindings[key]; !ok || b.ExitClaimName != t.Status.AssignedExit {
			return false
		}
	}

	// Inverse direction: cache has X but API doesn't.
	for id := range c.exits {
		if _, ok := apiClaimProviderIDs[id]; !ok {
			return false
		}
	}
	for name, id := range c.nameToProviderID {
		if _, ok := apiClaimNames[name]; !ok {
			return false
		}
		// If the cache thinks this name has a launched ProviderID but
		// the API has no claim with that ID, we're stale.
		if id != "" {
			if _, ok := apiClaimProviderIDs[id]; !ok {
				return false
			}
		}
	}
	for name := range c.pools {
		if _, ok := apiPoolNames[name]; !ok {
			return false
		}
	}
	for key, b := range c.bindings {
		assigned, ok := apiTunnelKeys[key]
		if !ok {
			return false
		}
		if b.ExitClaimName != assigned {
			return false
		}
	}
	return true
}
