# Phase 3: state.Cluster + Informer Controllers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the in-memory cluster cache (`*state.Cluster`) and the write-only informer controllers that keep it synced with the apiserver. Every later controller (provisioner, lifecycle, disruption) reads from `*state.Cluster` rather than calling the apiserver directly.

**Architecture:** One central `*state.Cluster` struct with a `sync.RWMutex`-protected map of `StateExit` indexed by ProviderID. Informer controllers (one per CRD) watch their resource and call `cluster.Update*`/`Delete*`. Read consumers gate on `cluster.Synced(ctx)` which performs a list-vs-cache reconciliation to ensure no informer lag.

**Tech Stack:** sigs.k8s.io/controller-runtime, ginkgo + gomega, envtest.

**Spec section reference:** §8 (state.Cluster).

**Prerequisites:** Phase 1 + 2 merged.

**End state:**
- `pkg/controllers/state/cluster.go` defines `Cluster` and `StateExit`.
- `pkg/controllers/state/informer/*` watches all four CRDs (ExitClaim, ExitPool, Tunnel, ProviderClass).
- `Cluster.Synced(ctx)` does the list-vs-cache reconciliation.
- `envtest`-based suite confirms cluster reflects API state for create/update/delete.
- `make test` passes for `./pkg/controllers/state/...`.

---

## File map

### Created

```
pkg/controllers/state/
├── cluster.go                            # Cluster struct, public API
├── stateexit.go                          # StateExit (joins ExitClaim + derived Allocations)
├── synced.go                             # Synced(ctx): list-vs-cache reconcile gate
├── statepool.go                          # StatePool: ExitPool + per-pool aggregate counters
├── statetunnel.go                        # tracks Tunnel-side bindings (assignedExit, assignedPorts)
├── doc.go
├── suite_test.go                         # ginkgo suite, envtest setup
├── cluster_test.go                       # core API tests
├── synced_test.go                        # synced gate tests
└── informer/
    ├── doc.go
    ├── exitclaim_controller.go           # watches ExitClaim → cluster.UpdateExit/DeleteExit
    ├── exitpool_controller.go            # watches ExitPool → cluster.UpdatePool/DeletePool
    ├── tunnel_controller.go              # watches Tunnel → cluster.UpdateTunnelBinding
    ├── providerclass_controller.go       # watches each registered ProviderClass kind
    └── suite_test.go                     # informer-only ginkgo suite, envtest
```

---

## Task 1: StateExit + StatePool + StateTunnel data types

**Files:**
- Create: `pkg/controllers/state/doc.go`
- Create: `pkg/controllers/state/stateexit.go`
- Create: `pkg/controllers/state/statepool.go`
- Create: `pkg/controllers/state/statetunnel.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package state holds the in-memory cluster cache (*Cluster) used by
// the provisioner, lifecycle, and disruption controllers as their
// single source of truth. Mirrors sigs.k8s.io/karpenter
// pkg/controllers/state/.
package state
```

- [ ] **Step 2: Write `stateexit.go`**

```go
package state

import (
	"sync"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// StateExit is the in-memory aggregate for one ExitClaim. The Allocations
// map is derived from Tunnels that have AssignedExit==Claim.Name; it is
// NOT persisted on the ExitClaim CR (per Karpenter convention).
type StateExit struct {
	mu    sync.RWMutex
	Claim *v1alpha1.ExitClaim

	// Allocations maps port → tunnel-key ("<ns>/<name>"). Updated by
	// state.Cluster as Tunnel statuses change. Local copy (not shared).
	Allocations map[int32]TunnelKey

	// MarkedForDeletion blocks new bindings while disruption queue
	// drains the exit.
	MarkedForDeletion bool

	// Nominated indicates this exit was selected by a recent Solve.
	// Used by disruption to ignore freshly-scheduled exits.
	Nominated bool

	// DisruptionCost is filled by the disruption controller while
	// computing consolidation candidates. Zeroed otherwise.
	DisruptionCost float64
}

// TunnelKey is "<namespace>/<name>".
type TunnelKey string

// Available returns Allocatable minus the resource sum of currently
// bound tunnels. Conservative: per-tunnel resource requests come from
// the persisted Tunnel.Spec.Resources.Requests.
func (s *StateExit) Available(boundTunnelRequests []corev1.ResourceList) corev1.ResourceList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Claim == nil {
		return nil
	}
	out := s.Claim.Status.Allocatable.DeepCopy()
	for _, req := range boundTunnelRequests {
		for k, v := range req {
			if cur, ok := out[k]; ok {
				cur.Sub(v)
				out[k] = cur
			}
		}
	}
	return out
}

// UsedPorts returns a snapshot copy of the allocated port set.
func (s *StateExit) UsedPorts() map[int32]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int32]struct{}, len(s.Allocations))
	for p := range s.Allocations {
		out[p] = struct{}{}
	}
	return out
}

// PortHolder returns the tunnel-key bound to the given port, or empty.
func (s *StateExit) PortHolder(p int32) TunnelKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Allocations[p]
}

// IsEmpty reports whether no tunnels are bound to this exit.
func (s *StateExit) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Allocations) == 0
}

// snapshotForRead returns a goroutine-safe deep copy of the underlying
// claim and a clone of the allocations map. Helpers for callers who
// need to read both atomically.
func (s *StateExit) snapshotForRead() (*v1alpha1.ExitClaim, map[int32]TunnelKey) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Claim == nil {
		return nil, nil
	}
	allocCopy := make(map[int32]TunnelKey, len(s.Allocations))
	for k, v := range s.Allocations {
		allocCopy[k] = v
	}
	return s.Claim.DeepCopy(), allocCopy
}
```

- [ ] **Step 3: Write `statepool.go`**

```go
package state

import (
	"sync"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// StatePool aggregates one ExitPool + the running totals of resources
// consumed by exits the pool has produced. Counter controller updates
// these in Phase 7; for now Phase 3 stores the pool object only.
type StatePool struct {
	mu        sync.RWMutex
	Pool      *v1alpha1.ExitPool
	Resources corev1.ResourceList
	Exits     int64
}

func (p *StatePool) Snapshot() *v1alpha1.ExitPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Pool == nil {
		return nil
	}
	return p.Pool.DeepCopy()
}
```

- [ ] **Step 4: Write `statetunnel.go`**

```go
package state

// TunnelBinding records the live assignment of a Tunnel to an ExitClaim
// (denormalized from Tunnel.Status). Cluster.Bindings is keyed by
// "<ns>/<name>"; the value points at an ExitClaim by name (cluster-scoped
// CR so namespace not relevant).
type TunnelBinding struct {
	TunnelKey      TunnelKey
	ExitClaimName  string
	AssignedPorts  []int32
}
```

- [ ] **Step 5: Run go vet**

Run: `go vet ./pkg/controllers/state/...`
Expected: PASS, no implementations consume them yet.

- [ ] **Step 6: Don't commit yet — Task 2 builds Cluster.**

---

## Task 2: Cluster public API

**Files:**
- Create: `pkg/controllers/state/cluster.go`

- [ ] **Step 1: Write `cluster.go`**

```go
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

func (c *Cluster) bumpAndFire() {
	c.clusterState = time.Now()
	if c.provisionerTrigger != nil {
		c.provisionerTrigger()
	}
	if c.disruptionTrigger != nil {
		c.disruptionTrigger()
	}
}

// ----- ExitClaim API -----

func (c *Cluster) UpdateExit(claim *v1alpha1.ExitClaim) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := claim.Status.ProviderID
	if id == "" {
		// Claim hasn't been launched yet; index by name only.
		c.nameToProviderID[claim.Name] = ""
		return
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
	c.bumpAndFire()
}

func (c *Cluster) DeleteExit(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.nameToProviderID[name]
	if !ok {
		return
	}
	delete(c.exits, id)
	delete(c.nameToProviderID, name)
	c.bumpAndFire()
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
	defer c.mu.Unlock()
	sp, ok := c.pools[pool.Name]
	if !ok {
		sp = &StatePool{}
		c.pools[pool.Name] = sp
	}
	sp.mu.Lock()
	sp.Pool = pool.DeepCopy()
	sp.mu.Unlock()
	c.bumpAndFire()
}

func (c *Cluster) DeletePool(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pools, name)
	c.bumpAndFire()
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
	defer c.mu.Unlock()
	if exitName == "" {
		// Unbind: clear binding and remove ports from any allocation.
		old := c.bindings[key]
		delete(c.bindings, key)
		if old != nil {
			c.recomputeAllocationsLocked(old.ExitClaimName)
		}
		c.bumpAndFire()
		return
	}
	c.bindings[key] = &TunnelBinding{
		TunnelKey:     key,
		ExitClaimName: exitName,
		AssignedPorts: append([]int32(nil), ports...),
	}
	c.recomputeAllocationsLocked(exitName)
	c.bumpAndFire()
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
```

- [ ] **Step 2: Run go vet + build**

Run: `go vet ./pkg/controllers/state/... && go build ./pkg/controllers/state/...`
Expected: PASS.

- [ ] **Step 3: Don't commit yet — Task 4 has tests.**

---

## Task 3: Synced(ctx) gate

**Files:**
- Create: `pkg/controllers/state/synced.go`

- [ ] **Step 1: Write `synced.go`**

```go
package state

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

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
```

- [ ] **Step 2: Run go vet + build**

Run: `go vet ./pkg/controllers/state/... && go build ./pkg/controllers/state/...`
Expected: PASS.

- [ ] **Step 3: Don't commit yet — Task 4 has tests.**

---

## Task 4: Cluster + Synced unit tests

**Files:**
- Create: `pkg/controllers/state/suite_test.go`
- Create: `pkg/controllers/state/cluster_test.go`
- Create: `pkg/controllers/state/synced_test.go`

- [ ] **Step 1: Write `suite_test.go`**

```go
package state_test

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
)

func TestState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "state cluster suite")
}

var _ = BeforeSuite(func() {
	By("starting envtest")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(v1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(ldv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(dov1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("stopping envtest")
	Expect(testEnv.Stop()).To(Succeed())
})
```

- [ ] **Step 2: Write `cluster_test.go`**

```go
package state_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func newClaim(name, providerID string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: providerID,
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("1000"),
			},
		},
	}
}

var _ = Describe("Cluster.UpdateExit / DeleteExit", func() {
	It("indexes by ProviderID and Name", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		Expect(c.ExitForProviderID("fake://abc")).NotTo(BeNil())
		Expect(c.ExitForName("e1")).NotTo(BeNil())

		c.DeleteExit("e1")
		Expect(c.ExitForProviderID("fake://abc")).To(BeNil())
		Expect(c.ExitForName("e1")).To(BeNil())
	})
})

var _ = Describe("Cluster.UpdateTunnelBinding", func() {
	It("derives StateExit.Allocations from bindings", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		c.UpdateTunnelBinding("default/svc-a", "e1", []int32{80})

		se := c.ExitForName("e1")
		Expect(se).NotTo(BeNil())
		Expect(se.UsedPorts()).To(HaveKey(int32(80)))
		Expect(se.PortHolder(80)).To(BeEquivalentTo("default/svc-a"))
	})

	It("clears allocations on unbind", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		c.UpdateTunnelBinding("default/svc-a", "e1", []int32{80})
		c.DeleteTunnelBinding("default/svc-a")

		se := c.ExitForName("e1")
		Expect(se.IsEmpty()).To(BeTrue())
	})
})
```

- [ ] **Step 3: Write `synced_test.go`**

```go
package state_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

var _ = Describe("Cluster.Synced", func() {
	It("returns true for empty cluster + empty cache", func() {
		c := state.NewCluster(k8sClient)
		Expect(c.Synced(context.Background())).To(BeTrue())
	})

	It("returns false when claim exists in API but not cache", func() {
		ctx := context.Background()
		claim := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "e-out-of-sync"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps: v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		// Patch ProviderID via status update.
		claim.Status.ProviderID = "fake://uut"
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, claim) }()

		c := state.NewCluster(k8sClient)
		Expect(c.Synced(ctx)).To(BeFalse())
	})
})
```

- [ ] **Step 4: Run; verify PASS**

Run: `KUBEBUILDER_ASSETS=$(setup-envtest use 1.31.x -p path) go test ./pkg/controllers/state/ -v`
Expected: all specs PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/controllers/state/
git commit -m "feat(state): in-memory cluster cache + Synced(ctx) gate"
```

---

## Task 5: Informer controllers

**Files:**
- Create: `pkg/controllers/state/informer/doc.go`
- Create: `pkg/controllers/state/informer/exitclaim_controller.go`
- Create: `pkg/controllers/state/informer/exitpool_controller.go`
- Create: `pkg/controllers/state/informer/tunnel_controller.go`
- Create: `pkg/controllers/state/informer/providerclass_controller.go`
- Create: `pkg/controllers/state/informer/suite_test.go`
- Create: `pkg/controllers/state/informer/exitclaim_controller_test.go`
- Create: `pkg/controllers/state/informer/tunnel_controller_test.go`

Each controller is **write-only**: it watches one CRD and pushes changes into `*state.Cluster`. No status writes back to apiserver here.

- [ ] **Step 1: Write `doc.go`**

```go
// Package informer contains write-only controllers that keep the
// state.Cluster cache synced with apiserver. One per CRD.
package informer
```

- [ ] **Step 2: Write `exitclaim_controller.go`**

```go
package informer

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ExitClaimController syncs ExitClaim → state.Cluster.
type ExitClaimController struct {
	client.Client
	Cluster *state.Cluster
}

func (r *ExitClaimController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var claim v1alpha1.ExitClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cluster.DeleteExit(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Cluster.UpdateExit(&claim)
	return ctrl.Result{}, nil
}

func (r *ExitClaimController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-exitclaim").
		For(&v1alpha1.ExitClaim{}).
		Complete(r)
}
```

- [ ] **Step 3: Write `exitpool_controller.go`** — same shape, swaps `ExitClaim`→`ExitPool` and `UpdateExit`→`UpdatePool`.

- [ ] **Step 4: Write `tunnel_controller.go`**

```go
package informer

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

type TunnelController struct {
	client.Client
	Cluster *state.Cluster
}

func (r *TunnelController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var t v1alpha1.Tunnel
	key := state.TunnelKey(fmt.Sprintf("%s/%s", req.Namespace, req.Name))
	if err := r.Get(ctx, req.NamespacedName, &t); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cluster.DeleteTunnelBinding(key)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Cluster.UpdateTunnelBinding(key, t.Status.AssignedExit, t.Status.AssignedPorts)
	return ctrl.Result{}, nil
}

func (r *TunnelController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-tunnel").
		For(&v1alpha1.Tunnel{}).
		Complete(r)
}
```

- [ ] **Step 5: Write `providerclass_controller.go`**

```go
package informer

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ProviderClassController is a no-op for now: it watches every
// registered ProviderClass kind so the informer cache is warm before
// dependent controllers (lifecycle, scheduler) read them. State.Cluster
// does not yet need ProviderClass entries — Phase 5+ adds caching if
// needed.
type ProviderClassController struct {
	client.Client
	Cluster *state.Cluster
	Watch   client.Object // the typed kind to watch (e.g. &ldv1alpha1.LocalDockerProviderClass{})
}

func (r *ProviderClassController) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ProviderClassController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-providerclass-" + r.Watch.GetObjectKind().GroupVersionKind().Kind).
		For(r.Watch).
		Complete(r)
}
```

- [ ] **Step 6: Tests (`exitclaim_controller_test.go`, `tunnel_controller_test.go`, suite_test.go)**

Pattern: spin up envtest in BeforeSuite, run a manager + cluster + controllers, create/update/delete API objects, Eventually assert the cluster cache reflects.

```go
package informer_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

var _ = Describe("ExitClaim informer", func() {
	It("propagates Create + ProviderID into Cluster", func() {
		ctx := context.Background()
		claim := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "informer-e1"},
			Spec: v1alpha1.ExitClaimSpec{
				ProviderClassRef: v1alpha1.ProviderClassRef{Group: "frp.operator.io", Kind: "FakeProviderClass", Name: "default"},
				Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
			},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, claim) }()

		claim.Status.ProviderID = "fake://informer-id"
		Expect(k8sClient.Status().Update(ctx, claim)).To(Succeed())

		Eventually(func() *state.StateExit {
			return cluster.ExitForName("informer-e1")
		}, "5s", "200ms").ShouldNot(BeNil())
	})
})
```

(`cluster` is a package-level `*state.Cluster` constructed in `suite_test.go` and wired to the running manager.)

- [ ] **Step 7: Run; verify PASS**

Run: `KUBEBUILDER_ASSETS=$(setup-envtest use 1.31.x -p path) go test ./pkg/controllers/state/informer/ -v`
Expected: all specs PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/controllers/state/informer/
git commit -m "feat(state/informer): write-only controllers sync apiserver → cluster cache"
```

---

## Phase 3 acceptance checklist

- [x] `pkg/controllers/state/cluster.go` provides `NewCluster`, `Update*`, `Delete*`, `Exits()`, `Pools()`, `Bindings()`, `Synced(ctx)`.
- [x] `pkg/controllers/state/stateexit.go` defines `StateExit` with derived `Allocations`, `Available()`, `UsedPorts()`, `IsEmpty()`.
- [x] `pkg/controllers/state/synced.go` performs list-vs-cache reconciliation across ExitClaim + ExitPool + Tunnel.
- [x] `pkg/controllers/state/informer/` has one write-only controller per CRD (ExitClaim, ExitPool, Tunnel, generic ProviderClass watcher).
- [x] envtest-based ginkgo suite passes for both `pkg/controllers/state/` and `pkg/controllers/state/informer/`.
- [x] `make test` passes for the new packages.
- [x] All commits compile clean (`go build`, `go vet`).

---

## Out-of-scope reminders

- Provisioner / scheduler — Phase 4. State.Cluster is read-only there.
- Lifecycle controller — Phase 5.
- Disruption — Phase 6.
- Pool counter / hash / validation — Phase 7. Phase 3 stores ExitPool but doesn't roll up resources or stamp hashes.
- `*ProviderClass` per-provider config CRDs — registered watchers exist but don't cache anything yet. Phase 5 adds typed access if needed.
- Real informer-cache-bypass with APIReader — not used. We trust the informer once `Synced(ctx)` returns true.
