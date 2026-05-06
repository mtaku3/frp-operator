package state

import (
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Cluster is the in-memory truth used by all decision-making
// controllers. Goroutine-safe.
type Cluster struct {
	mu sync.RWMutex

	// exits keyed by ProviderID (canonical identity).
	exits map[string]*StateExit

	// nameToProviderID gives us O(1) lookup by ExitClaim name.
	nameToProviderID map[string]string

	// pendingClaims keyed by name, holds claims observed via informer
	// before the lifecycle controller has launched them (Status.ProviderID
	// still empty). Without this index the scheduler cannot see freshly-
	// persisted claims when binpacking, leading to duplicate-claim races
	// across separate Solve runs (see issue #7).
	pendingClaims map[string]*v1alpha1.ExitClaim

	// pools keyed by name.
	pools map[string]*StatePool

	// bindings keyed by "<ns>/<name>" of Tunnel.
	bindings map[TunnelKey]*TunnelBinding

	// clusterState is bumped on any cache mutation; used by
	// disruption to skip when state hasn't changed.
	clusterState time.Time

	// kubeClient is the live API client used by Synced(ctx) to
	// list-vs-cache reconcile.
	kubeClient client.Client

	// triggers fired on relevant change.
	provisionerTrigger func()
	disruptionTrigger  func()
}

// NewCluster constructs a fresh Cluster bound to the given client.
func NewCluster(kube client.Client) *Cluster {
	return &Cluster{
		exits:            map[string]*StateExit{},
		nameToProviderID: map[string]string{},
		pendingClaims:    map[string]*v1alpha1.ExitClaim{},
		pools:            map[string]*StatePool{},
		bindings:         map[TunnelKey]*TunnelBinding{},
		kubeClient:       kube,
	}
}

// SetTriggers wires the trigger callbacks. Called by operator wiring
// after both the cluster and the provisioner/disruption controllers
// are constructed.
func (c *Cluster) SetTriggers(provisioner, disruption func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provisionerTrigger = provisioner
	c.disruptionTrigger = disruption
}

// bumpAndCollectTriggers updates clusterState and returns the trigger
// functions captured under the lock. Callers MUST invoke the returned
// triggers AFTER releasing c.mu to avoid re-entrant deadlocks (a
// future trigger may read back into Cluster).
func (c *Cluster) bumpAndCollectTriggers() (provisioner, disruption func()) {
	c.clusterState = time.Now()
	return c.provisionerTrigger, c.disruptionTrigger
}

// fireTriggers invokes the captured trigger fns. Safe to call with nil
// values.
func fireTriggers(provisioner, disruption func()) {
	if provisioner != nil {
		provisioner()
	}
	if disruption != nil {
		disruption()
	}
}

// ----- ExitClaim API -----

func (c *Cluster) UpdateExit(claim *v1alpha1.ExitClaim) {
	c.mu.Lock()
	id := claim.Status.ProviderID
	if id == "" {
		// Claim hasn't been launched yet; index by name + retain a copy
		// in pendingClaims so the scheduler can rehydrate it when
		// binpacking (issue #7).
		c.nameToProviderID[claim.Name] = ""
		c.pendingClaims[claim.Name] = claim.DeepCopy()
		p, d := c.bumpAndCollectTriggers()
		c.mu.Unlock()
		fireTriggers(p, d)
		return
	}
	// Promotion: claim has graduated from pending → launched.
	delete(c.pendingClaims, claim.Name)
	// If the same name previously pointed at a different ProviderID,
	// drop the orphaned entry from c.exits before writing the new one.
	if oldID, ok := c.nameToProviderID[claim.Name]; ok && oldID != "" && oldID != id {
		delete(c.exits, oldID)
	}
	se, ok := c.exits[id]
	if !ok {
		se = &StateExit{Allocations: map[int32]TunnelKey{}}
		c.exits[id] = se
	}
	se.mu.Lock()
	se.Claim = claim.DeepCopy()
	se.mu.Unlock()
	c.nameToProviderID[claim.Name] = id
	// Re-derive allocations now that we have the claim's name. Same
	// data is also written by Tunnel events; this catches the
	// out-of-order arrival case.
	c.recomputeAllocationsLocked(claim.Name)
	p, d := c.bumpAndCollectTriggers()
	c.mu.Unlock()
	fireTriggers(p, d)
}

func (c *Cluster) DeleteExit(name string) {
	c.mu.Lock()
	id, ok := c.nameToProviderID[name]
	if !ok {
		// Pending-only path: claim was deleted before launch.
		if _, hadPending := c.pendingClaims[name]; hadPending {
			delete(c.pendingClaims, name)
			p, d := c.bumpAndCollectTriggers()
			c.mu.Unlock()
			fireTriggers(p, d)
			return
		}
		c.mu.Unlock()
		return
	}
	delete(c.exits, id)
	delete(c.nameToProviderID, name)
	delete(c.pendingClaims, name)
	p, d := c.bumpAndCollectTriggers()
	c.mu.Unlock()
	fireTriggers(p, d)
}

func (c *Cluster) ExitForProviderID(id string) *StateExit {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.exits[id]
}

func (c *Cluster) ExitForName(name string) *StateExit {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.nameToProviderID[name]
	if !ok {
		return nil
	}
	return c.exits[id]
}

// PendingClaims returns deep copies of all claims observed but not yet
// launched (Status.ProviderID empty). Used by the scheduler to rehydrate
// inflight slots so a Solve running before a previous Solve's claim
// reaches Ready does not mint a duplicate.
func (c *Cluster) PendingClaims() []*v1alpha1.ExitClaim {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*v1alpha1.ExitClaim, 0, len(c.pendingClaims))
	for _, p := range c.pendingClaims {
		out = append(out, p.DeepCopy())
	}
	return out
}

// PortsForClaimName returns the set of ports currently bound to the
// named ExitClaim, derived by scanning recorded Tunnel bindings. Works
// for pending claims (no StateExit/Allocations yet) and for launched
// ones; returns an empty map if the claim has no bindings.
func (c *Cluster) PortsForClaimName(name string) map[int32]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[int32]struct{}{}
	for _, b := range c.bindings {
		if b.ExitClaimName != name {
			continue
		}
		for _, p := range b.AssignedPorts {
			out[p] = struct{}{}
		}
	}
	return out
}

// Exits returns a snapshot of all known StateExits. Caller may safely
// iterate the returned slice concurrently with cluster mutations.
func (c *Cluster) Exits() []*StateExit {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*StateExit, 0, len(c.exits))
	for _, e := range c.exits {
		out = append(out, e)
	}
	return out
}

// ----- ExitPool API -----

func (c *Cluster) UpdatePool(pool *v1alpha1.ExitPool) {
	c.mu.Lock()
	sp, ok := c.pools[pool.Name]
	if !ok {
		sp = &StatePool{}
		c.pools[pool.Name] = sp
	}
	sp.mu.Lock()
	sp.Pool = pool.DeepCopy()
	sp.mu.Unlock()
	// Mirror the counter-controller's status rollup so scheduler hot-paths
	// (poolLimitsExceeded) can read the running totals without a status
	// fetch. Status is the source of truth; this just shadows it. The
	// setter takes its own lock and DeepCopies, so concurrent readers
	// using SnapshotResources never race on the underlying map.
	sp.setResources(pool.Status.Resources, pool.Status.Exits)
	p, d := c.bumpAndCollectTriggers()
	c.mu.Unlock()
	fireTriggers(p, d)
}

func (c *Cluster) DeletePool(name string) {
	c.mu.Lock()
	delete(c.pools, name)
	p, d := c.bumpAndCollectTriggers()
	c.mu.Unlock()
	fireTriggers(p, d)
}

func (c *Cluster) Pool(name string) *StatePool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pools[name]
}

func (c *Cluster) Pools() []*StatePool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*StatePool, 0, len(c.pools))
	for _, p := range c.pools {
		out = append(out, p)
	}
	return out
}

// ----- Tunnel binding API -----

func (c *Cluster) UpdateTunnelBinding(key TunnelKey, exitName string, ports []int32) {
	c.mu.Lock()
	old := c.bindings[key]
	if exitName == "" {
		// Unbind: clear binding and remove ports from any allocation.
		delete(c.bindings, key)
		if old != nil {
			c.recomputeAllocationsLocked(old.ExitClaimName)
		}
		p, d := c.bumpAndCollectTriggers()
		c.mu.Unlock()
		fireTriggers(p, d)
		return
	}
	c.bindings[key] = &TunnelBinding{
		TunnelKey:     key,
		ExitClaimName: exitName,
		AssignedPorts: append([]int32(nil), ports...),
	}
	// If the tunnel moved between exits, the OLD exit still has a
	// stale port mapping pointing at this tunnel — recompute it too.
	if old != nil && old.ExitClaimName != "" && old.ExitClaimName != exitName {
		c.recomputeAllocationsLocked(old.ExitClaimName)
	}
	c.recomputeAllocationsLocked(exitName)
	p, d := c.bumpAndCollectTriggers()
	c.mu.Unlock()
	fireTriggers(p, d)
}

func (c *Cluster) DeleteTunnelBinding(key TunnelKey) {
	c.UpdateTunnelBinding(key, "", nil)
}

// recomputeAllocationsLocked rebuilds the StateExit.Allocations for the
// named exit claim by scanning all bindings. Caller MUST hold c.mu.
func (c *Cluster) recomputeAllocationsLocked(exitName string) {
	id, ok := c.nameToProviderID[exitName]
	if !ok {
		return
	}
	se, ok := c.exits[id]
	if !ok {
		return
	}
	allocs := map[int32]TunnelKey{}
	for tunnelKey, binding := range c.bindings {
		if binding.ExitClaimName != exitName {
			continue
		}
		for _, p := range binding.AssignedPorts {
			allocs[p] = tunnelKey
		}
	}
	se.mu.Lock()
	se.Allocations = allocs
	se.mu.Unlock()
}

// BindingForTunnel returns the recorded binding (or nil).
func (c *Cluster) BindingForTunnel(key TunnelKey) *TunnelBinding {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bindings[key]
}

// ClusterState returns the monotonic version stamp.
func (c *Cluster) ClusterState() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clusterState
}

// MarkExitForDeletion sets MarkedForDeletion=true on the StateExit indexed by
// the given ExitClaim name. No-op if the exit is unknown. Used by the
// disruption controller to gate the provisioner away from a candidate before
// the actual API delete fires.
func (c *Cluster) MarkExitForDeletion(name string) {
	c.mu.RLock()
	id, ok := c.nameToProviderID[name]
	if !ok {
		c.mu.RUnlock()
		return
	}
	se, ok := c.exits[id]
	c.mu.RUnlock()
	if !ok || se == nil {
		return
	}
	se.mu.Lock()
	se.MarkedForDeletion = true
	se.mu.Unlock()
}
