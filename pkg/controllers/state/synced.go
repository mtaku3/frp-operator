package state

import (
	"context"
	"fmt"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Synced performs a list-vs-cache reconciliation: it lists ExitClaims,
// ExitPools, and Tunnels live, and confirms every entry has a
// corresponding cluster cache record.
//
// Returns true iff the cluster cache is consistent with apiserver state
// at the moment of the call. False signals callers to retry — the
// informer is lagging.
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

	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Status.ProviderID == "" {
			continue
		}
		if _, ok := c.exits[claim.Status.ProviderID]; !ok {
			return false
		}
	}
	for i := range pools.Items {
		if _, ok := c.pools[pools.Items[i].Name]; !ok {
			return false
		}
	}
	for i := range tunnels.Items {
		t := &tunnels.Items[i]
		if t.Status.AssignedExit == "" {
			continue
		}
		key := TunnelKey(fmt.Sprintf("%s/%s", t.Namespace, t.Name))
		if b, ok := c.bindings[key]; !ok || b.ExitClaimName != t.Status.AssignedExit {
			return false
		}
	}
	return true
}
