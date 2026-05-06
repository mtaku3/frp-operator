# Phase 4: Provisioning Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the singleton provisioner, the trigger-batcher, and the three-stage scheduler (`addToExistingExit` → `addToInflightClaim` → `addToNewClaim`). On a Tunnel-arrival burst, the provisioner debounces, runs one Solve over all pending tunnels, and either binds them to existing/inflight exits or creates new ExitClaim CRs. Lifecycle controller (Phase 5) handles the actual cloud-provider Create.

**Architecture:** `Provisioner` is a `singleton.AsReconciler` (no Request, manager calls `Reconcile(ctx)` repeatedly). `Batcher[T]` accumulates `Trigger(uid)` calls; `Wait(ctx)` returns when idle ≥ `BATCH_IDLE_DURATION` (1s) or total time ≥ `BATCH_MAX_DURATION` (10s). `Scheduler.Solve(pendingTunnels)` runs the three-stage pipeline. `pod_controller.go` (watches Tunnel) and `node_controller.go` (watches ExitClaim) call `Trigger` on relevant events. Reads from `state.Cluster`; writes ExitClaim CRs and Tunnel.Status.

**Spec:** §5 (provisioning loop), §3.1 (ExitPool template), §3.2 (ExitClaim spec), §11 (PREFERENCE_POLICY, MIN_VALUES_POLICY).

**Prerequisites:** Phases 1, 2, 3 merged.

**End state:**
- `pkg/controllers/provisioning/` produces ExitClaim CRs and patches Tunnel.Status when a Solve completes.
- `pkg/controllers/provisioning/scheduling/` is the pure-function scheduler.
- envtest-backed ginkgo suite verifies: a pending Tunnel with no eligible exits triggers an ExitClaim creation; a second pending Tunnel binpacks onto the inflight ExitClaim; a tunnel whose ports collide with allocations gets a new ExitClaim.
- `make test` passes for `./pkg/controllers/provisioning/...`.

---

## File map

```
pkg/controllers/provisioning/
├── doc.go
├── provisioner.go                       # Provisioner struct, Reconcile, Schedule, CreateExitClaims
├── provisioner_test.go                  # envtest-backed ginkgo suite for the singleton loop
├── batcher.go                           # generic Batcher[T comparable]
├── batcher_test.go                      # unit tests for batching/idle/max windows
├── pod_controller.go                    # watches Tunnel → Provisioner.Trigger
├── node_controller.go                   # watches ExitClaim → Provisioner.Trigger
├── controllers_test.go                  # the trigger controllers light-up the batcher
├── suite_test.go                        # envtest setup, scheme registration
└── scheduling/
    ├── doc.go
    ├── scheduler.go                     # Scheduler struct, Solve, add, addToExistingExit, addToInflightClaim, addToNewClaim
    ├── scheduler_test.go                # unit tests against fake state (no envtest)
    ├── existing_exit.go                 # ExistingExit (wraps state.StateExit), CanAdd
    ├── inflight_claim.go                # *ExitClaim helper for in-flight Solves; CanAdd; tracks bound tunnels
    ├── new_claim.go                     # build a fresh ExitClaim from an ExitPool template
    ├── requirements.go                  # NodeSelectorRequirementWithMinValues helpers — Compatible, Intersects
    ├── requirements_test.go
    ├── ports.go                         # PortsFit predicate (discrete port-conflict check)
    ├── ports_test.go
    ├── resources.go                     # ResourcesFit predicate (corev1.ResourceList subtraction)
    ├── resources_test.go
    ├── preferences.go                   # Relax(tunnel) — peels off optional requirements one at a time
    └── results.go                       # Results{NewClaims, Bindings, TunnelErrors}
```

---

## Task 1: scheduling/requirements + ports + resources predicates (pure functions, no I/O)

**Files:**
- Create: `pkg/controllers/provisioning/scheduling/doc.go`, `requirements.go`, `requirements_test.go`, `ports.go`, `ports_test.go`, `resources.go`, `resources_test.go`.

These three predicates are the heart of `CanAdd`. Pure functions — TDD-friendly.

- [ ] **doc.go** — one-paragraph package comment: "scheduling implements the three-stage Solve pipeline. Mirrors sigs.k8s.io/karpenter pkg/controllers/provisioning/scheduling."

- [ ] **requirements.go**

```go
package scheduling

import (
    "fmt"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Requirements is a typed slice with helpers.
type Requirements []v1alpha1.NodeSelectorRequirementWithMinValues

// Compatible returns true iff every requirement in `pool` is satisfied by
// at least one requirement in `tunnel` (intersection non-empty for matching
// keys; missing keys treated as wildcard).
func Compatible(pool, tunnel Requirements) error {
    // For each key the pool constrains, find any tunnel constraint on the same key.
    // If both constrain the same key, the value sets must intersect for In/NotIn,
    // and the comparator must agree for Gt/Lt.
    // If pool requires Exists and tunnel says DoesNotExist, fail.
    // Implementation: walk pool, lookup tunnel by key, check operator-specific compatibility.
    for _, p := range pool {
        match := false
        for _, t := range tunnel {
            if t.Key != p.Key { continue }
            if !operatorsCompatible(p, t) {
                return fmt.Errorf("requirement %s: pool %v vs tunnel %v incompatible", p.Key, p, t)
            }
            match = true
        }
        _ = match
    }
    return nil
}

func operatorsCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
    // implement per-operator pair-table:
    // In/In → values intersect
    // In/NotIn → values disjoint and pool's set has at least one value not in tunnel.NotIn
    // Exists/* → DoesNotExist incompatible
    // Gt/Lt → numeric range check
    // ... (see Karpenter pkg/scheduling/requirements.go for canonical impl)
    return true // placeholder; subagent fills in
}
```

Tests in `requirements_test.go` cover at minimum: same-key In/In intersect, same-key In/In disjoint=fail, Exists vs DoesNotExist=fail, missing-key=ok, Gt/Lt numeric.

- [ ] **ports.go**

```go
package scheduling

import (
    "strconv"
    "strings"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PortsFit reports whether every requested port can be allocated on the
// exit. It walks the ExitClaim's AllowPorts \ ReservedPorts \ usedPorts
// for each requested port. PublicPort=0 means "auto-assign from
// remaining"; the caller resolves this via ResolveAutoAssign below.
func PortsFit(allowPorts []string, reserved []int32, used map[int32]struct{}, requested []v1alpha1.TunnelPort) bool {
    free := computeFree(allowPorts, reserved, used)
    needAuto := 0
    for _, p := range requested {
        if p.PublicPort == nil || *p.PublicPort == 0 {
            needAuto++
            continue
        }
        if _, ok := free[*p.PublicPort]; !ok {
            return false
        }
        delete(free, *p.PublicPort)
    }
    return len(free) >= needAuto
}

// ResolveAutoAssign returns a port assignment for each requested port,
// substituting concrete numbers for any PublicPort=0/nil entries. Returns
// nil if not allocatable.
func ResolveAutoAssign(allowPorts []string, reserved []int32, used map[int32]struct{}, requested []v1alpha1.TunnelPort) ([]int32, bool) { /* ... */ }

// computeFree expands AllowPorts ranges minus Reserved minus used.
func computeFree(allowPorts []string, reserved []int32, used map[int32]struct{}) map[int32]struct{} { /* ... */ }
```

Tests cover specific-port hit/miss, range expansion, auto-assign one slot, auto-assign exceeds availability.

- [ ] **resources.go**

```go
package scheduling

import (
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/resource"
)

// ResourcesFit reports whether `requested` ResourceList can be subtracted
// from `available` without going negative on any dimension. Missing
// dimensions in `available` mean "unbounded for that dimension."
// Recognized dimensions: cpu, memory, frp.operator.io/bandwidthMbps,
// frp.operator.io/monthlyTrafficGB.
func ResourcesFit(available, requested corev1.ResourceList) bool {
    for k, req := range requested {
        avail, ok := available[k]
        if !ok { continue } // unbounded
        if avail.Cmp(req) < 0 { return false }
    }
    return true
}

// Subtract returns available - requested for the dimensions in available.
func Subtract(available, requested corev1.ResourceList) corev1.ResourceList {
    out := corev1.ResourceList{}
    for k, v := range available {
        cur := v.DeepCopy()
        if r, ok := requested[k]; ok { cur.Sub(r) }
        out[k] = cur
    }
    return out
}

// Sum returns the dimension-wise sum.
func Sum(lists ...corev1.ResourceList) corev1.ResourceList {
    out := corev1.ResourceList{}
    for _, l := range lists {
        for k, v := range l {
            cur, ok := out[k]
            if !ok { out[k] = v.DeepCopy(); continue }
            cur.Add(v); out[k] = cur
        }
    }
    return out
}
```

Tests: simple subtraction non-negative, missing dim = ok, sum of two lists.

Commit at the end of Task 1: `feat(scheduling): pure-function predicates (requirements, ports, resources)`.

---

## Task 2: scheduling/{existing_exit, inflight_claim, new_claim, results, preferences}

These wrap the predicates with state and produce per-stage decisions.

- [ ] **results.go**

```go
package scheduling

import v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"

// Results is what Solve returns.
type Results struct {
    // NewClaims are speculative ExitClaim CRs the provisioner should
    // create. Each has its name + spec ready to send to the apiserver.
    NewClaims []*InflightClaim
    // Bindings record (tunnel, exit-claim-name, resolved-ports) for
    // tunnels successfully placed onto an existing or inflight claim.
    Bindings []Binding
    // TunnelErrors tracks tunnels that couldn't be scheduled with the
    // reason. Provisioner surfaces these as Tunnel.Status conditions.
    TunnelErrors map[string]error
}

type Binding struct {
    TunnelKey      string
    ExitClaimName  string
    AssignedPorts  []int32
}

// AllScheduled reports whether every input tunnel has a binding (or new claim) recorded.
func (r *Results) AllScheduled(inputTunnels int) bool {
    return len(r.Bindings) == inputTunnels
}
```

- [ ] **existing_exit.go**

```go
package scheduling

import (
    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ExistingExit wraps state.StateExit with a per-Solve mutable record of
// "tunnels we'd bind onto it during this Solve" so subsequent CanAdd
// calls see consumed capacity.
type ExistingExit struct {
    State           *state.StateExit
    NewlyBound      []*v1alpha1.Tunnel
    NewlyUsedPorts  map[int32]struct{}
}

func (e *ExistingExit) CanAdd(tunnel *v1alpha1.Tunnel) ([]int32, error) {
    // 1. RequirementsCompatible(claim.Spec.Requirements, tunnel.Spec.Requirements)
    // 2. ResourcesFit(state.Available - sum(NewlyBound requests), tunnel.Spec.Resources.Requests)
    // 3. PortsFit(claim.Frps.AllowPorts, claim.Frps.ReservedPorts, used ∪ NewlyUsedPorts, tunnel.Spec.Ports)
    // 4. liveness gates: state.Claim.Status.Conditions[Ready]=True, !state.MarkedForDeletion, do-not-disrupt OK.
    // Returns assignedPorts on success, error otherwise.
    panic("subagent: implement")
}

func (e *ExistingExit) Add(tunnel *v1alpha1.Tunnel, assignedPorts []int32) {
    e.NewlyBound = append(e.NewlyBound, tunnel)
    if e.NewlyUsedPorts == nil { e.NewlyUsedPorts = map[int32]struct{}{} }
    for _, p := range assignedPorts { e.NewlyUsedPorts[p] = struct{}{} }
}
```

- [ ] **inflight_claim.go**

```go
package scheduling

import (
    "sort"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// InflightClaim is a not-yet-persisted ExitClaim being assembled inside
// one Solve run. Subsequent tunnels in the same Solve can pack onto it
// via addToInflightClaim.
type InflightClaim struct {
    Spec        v1alpha1.ExitClaimSpec
    Name        string                  // deterministic, derived from pool + UID hash
    Pool        *v1alpha1.ExitPool      // the template
    Tunnels     []*v1alpha1.Tunnel      // bound during this Solve
    UsedPorts   map[int32]struct{}
}

func (c *InflightClaim) CanAdd(tunnel *v1alpha1.Tunnel) ([]int32, error) {
    // Same three predicates as ExistingExit, but capacity comes from the
    // pool's InstanceType.Allocatable (subagent: pull from
    // cloudProvider.GetInstanceTypes — but for simplicity in v1, the
    // template's Frps.AllowPorts and a hardcoded "always fits" capacity
    // is acceptable; capacity-aware binpacking is a Phase 7 refinement).
    panic("subagent: implement")
}

func (c *InflightClaim) Add(tunnel *v1alpha1.Tunnel, assignedPorts []int32) {
    c.Tunnels = append(c.Tunnels, tunnel)
    if c.UsedPorts == nil { c.UsedPorts = map[int32]struct{}{} }
    for _, p := range assignedPorts { c.UsedPorts[p] = struct{}{} }
}

// SortByLoad sorts a slice of inflight claims so least-loaded comes
// first (binpack tighter onto already-populated claims). Mirrors
// Karpenter's sort in scheduling/nodeclaim.go.
func SortByLoad(claims []*InflightClaim) {
    sort.SliceStable(claims, func(i, j int) bool {
        return len(claims[i].Tunnels) < len(claims[j].Tunnels)
    })
}
```

- [ ] **new_claim.go**

```go
package scheduling

import (
    "crypto/sha256"
    "encoding/hex"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// NewClaimFromPool builds an *InflightClaim from a pool template.
// Name is deterministic: <pool-name>-<8-char-hash(uid|pool-name|salt)>.
// Salt is derived from the current Solve's invocation timestamp (or
// caller-supplied), so two Solves get different names — but within one
// Solve, every NewClaimFromPool call for the same pool produces the
// same name (we don't actually reuse — caller checks via the
// InflightClaim list before constructing).
func NewClaimFromPool(pool *v1alpha1.ExitPool, salt string) *InflightClaim {
    h := sha256.Sum256([]byte(pool.Name + "|" + salt))
    name := pool.Name + "-" + hex.EncodeToString(h[:])[:8]
    spec := v1alpha1.ExitClaimSpec{
        ProviderClassRef:       pool.Spec.Template.Spec.ProviderClassRef,
        Requirements:           pool.Spec.Template.Spec.Requirements,
        Frps:                   pool.Spec.Template.Spec.Frps,
        ExpireAfter:            pool.Spec.Template.Spec.ExpireAfter,
        TerminationGracePeriod: pool.Spec.Template.Spec.TerminationGracePeriod,
    }
    return &InflightClaim{Spec: spec, Name: name, Pool: pool}
}
```

- [ ] **preferences.go**

```go
package scheduling

import v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"

// Preferences peels off optional requirements one at a time on retry.
// Use case: tunnel prefers region us-east-1 but doesn't require it.
// Karpenter calls this scheduling.Preferences.Relax(pod).
type Preferences struct{ Policy string /* "Respect" | "Ignore" */ }

// Relax drops one preferred requirement from the tunnel and returns true
// if anything was dropped. False means no further relaxation possible —
// the failure is permanent for this tunnel.
func (p *Preferences) Relax(tunnel *v1alpha1.Tunnel) bool {
    // v1: no preferred-vs-required distinction in our CRD yet.
    // Returns false always. Phase 6+ may add preferred requirements
    // (e.g. a `kind: PreferredAffinity` annotation), then this becomes
    // a real loop.
    return false
}
```

Tests for each file — minimum viable: NewClaimFromPool determinism, ExistingExit.CanAdd accepts/rejects per predicate combination, InflightClaim.CanAdd packs same exit twice, SortByLoad orders ascending.

Commit: `feat(scheduling): existing/inflight/new-claim helpers + Results`.

---

## Task 3: scheduling/scheduler.go (the Solve pipeline)

**Files:**
- Create: `pkg/controllers/provisioning/scheduling/scheduler.go`, `scheduler_test.go`.

```go
package scheduling

import (
    "context"
    "fmt"
    "time"

    "sigs.k8s.io/controller-runtime/pkg/client"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/cloudprovider"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Scheduler runs Solve over a batch of pending tunnels.
type Scheduler struct {
    Cluster       *state.Cluster
    CloudProvider *cloudprovider.Registry
    KubeClient    client.Client
    Preferences   *Preferences

    // mutable per-Solve:
    existingExits  []*ExistingExit
    inflightClaims []*InflightClaim
    pools          []*v1alpha1.ExitPool
    salt           string
}

func New(c *state.Cluster, cp *cloudprovider.Registry, kube client.Client) *Scheduler {
    return &Scheduler{Cluster: c, CloudProvider: cp, KubeClient: kube, Preferences: &Preferences{Policy: "Respect"}}
}

// Solve produces a Results from a list of tunnels.
func (s *Scheduler) Solve(ctx context.Context, tunnels []*v1alpha1.Tunnel) (Results, error) {
    s.salt = fmt.Sprintf("%d", time.Now().UnixNano())
    s.existingExits = nil
    s.inflightClaims = nil
    s.pools = nil

    // Snapshot existing exits from cluster cache.
    for _, se := range s.Cluster.Exits() {
        s.existingExits = append(s.existingExits, &ExistingExit{State: se})
    }
    // Snapshot pools.
    for _, sp := range s.Cluster.Pools() {
        s.pools = append(s.pools, sp.Snapshot())
    }
    // Sort pools by Weight desc.
    sortPoolsByWeight(s.pools)

    var results Results
    results.TunnelErrors = map[string]error{}
    for _, t := range tunnels {
        if err := s.add(ctx, t, &results); err != nil {
            // Try preference relaxation.
            if !s.Preferences.Relax(t) {
                results.TunnelErrors[tunnelKey(t)] = err
                continue
            }
            if err := s.add(ctx, t, &results); err != nil {
                results.TunnelErrors[tunnelKey(t)] = err
            }
        }
    }
    results.NewClaims = s.inflightClaims
    return results, nil
}

func (s *Scheduler) add(ctx context.Context, t *v1alpha1.Tunnel, r *Results) error {
    if err := s.addToExistingExit(t, r); err == nil { return nil }
    if err := s.addToInflightClaim(t, r); err == nil { return nil }
    return s.addToNewClaim(t, r)
}

func (s *Scheduler) addToExistingExit(t *v1alpha1.Tunnel, r *Results) error {
    for _, e := range s.existingExits {
        if assigned, err := e.CanAdd(t); err == nil {
            e.Add(t, assigned)
            r.Bindings = append(r.Bindings, Binding{TunnelKey: tunnelKey(t), ExitClaimName: e.State.Claim.Name, AssignedPorts: assigned})
            return nil
        }
    }
    return fmt.Errorf("no existing exit fits")
}

func (s *Scheduler) addToInflightClaim(t *v1alpha1.Tunnel, r *Results) error {
    SortByLoad(s.inflightClaims)
    for _, c := range s.inflightClaims {
        if assigned, err := c.CanAdd(t); err == nil {
            c.Add(t, assigned)
            r.Bindings = append(r.Bindings, Binding{TunnelKey: tunnelKey(t), ExitClaimName: c.Name, AssignedPorts: assigned})
            return nil
        }
    }
    return fmt.Errorf("no inflight claim fits")
}

func (s *Scheduler) addToNewClaim(t *v1alpha1.Tunnel, r *Results) error {
    for _, pool := range s.pools {
        // Check pool requirements compatible with tunnel.
        if err := Compatible(pool.Spec.Template.Spec.Requirements, t.Spec.Requirements); err != nil { continue }
        // Check pool Limits not yet exceeded.
        if exceeded, dim := poolLimitsExceeded(pool, s.Cluster); exceeded {
            r.TunnelErrors[tunnelKey(t)] = fmt.Errorf("pool %q limit %s exceeded", pool.Name, dim)
            continue
        }
        // Build a fresh inflight claim.
        c := NewClaimFromPool(pool, s.salt+"|"+tunnelKey(t))
        if assigned, err := c.CanAdd(t); err == nil {
            c.Add(t, assigned)
            s.inflightClaims = append(s.inflightClaims, c)
            r.Bindings = append(r.Bindings, Binding{TunnelKey: tunnelKey(t), ExitClaimName: c.Name, AssignedPorts: assigned})
            return nil
        }
    }
    return fmt.Errorf("no pool can produce a claim for tunnel %s", tunnelKey(t))
}

func sortPoolsByWeight(pools []*v1alpha1.ExitPool) { /* ascending? descending? karpenter is desc */ }
func poolLimitsExceeded(pool *v1alpha1.ExitPool, c *state.Cluster) (bool, string) { /* check c.Pool(pool.Name).Resources vs pool.Spec.Limits */ }
func tunnelKey(t *v1alpha1.Tunnel) string { return t.Namespace + "/" + t.Name }
```

Tests in `scheduler_test.go` (pure unit, no envtest — use a fresh `state.NewCluster(nil)`):

1. Solve over a single tunnel with no exits + no pools → tunnel ends up in `TunnelErrors`.
2. Solve with one pool, no exits → produces 1 NewClaim, 1 Binding.
3. Solve with two tunnels and one inflight claim emerging from tunnel 1 → tunnel 2 binpacks onto the inflight claim (single NewClaim, two bindings).
4. Solve with one Ready exit eligible → existing-exit binding, no NewClaim.
5. Solve with one Ready exit, ports collide → NewClaim is produced.
6. Pool Limits exceeded → tunnel ends in TunnelErrors with "limit exceeded".

Commit: `feat(scheduling): three-stage Solve pipeline + scheduler tests`.

---

## Task 4: batcher.go

```go
package provisioning

import (
    "context"
    "sync"
    "time"
)

const (
    DefaultBatchIdleDuration = 1 * time.Second
    DefaultBatchMaxDuration  = 10 * time.Second
)

// Batcher accumulates Trigger events. Wait blocks until idle ≥ idle
// or total elapsed ≥ max. Returns true if any triggers were observed.
type Batcher[T comparable] struct {
    mu        sync.Mutex
    pending   map[T]struct{}
    triggers  chan struct{}
    idle, max time.Duration
}

func NewBatcher[T comparable](idle, max time.Duration) *Batcher[T] {
    return &Batcher[T]{pending: map[T]struct{}{}, triggers: make(chan struct{}, 1), idle: idle, max: max}
}

func (b *Batcher[T]) Trigger(t T) {
    b.mu.Lock()
    b.pending[t] = struct{}{}
    b.mu.Unlock()
    select { case b.triggers <- struct{}{}: default: }
}

// Wait blocks until idle/max elapses. Returns true if any triggers observed.
func (b *Batcher[T]) Wait(ctx context.Context) bool {
    select {
    case <-ctx.Done(): return false
    case <-b.triggers:
    }
    deadline := time.NewTimer(b.max)
    idleTimer := time.NewTimer(b.idle)
    defer deadline.Stop()
    defer idleTimer.Stop()
    for {
        select {
        case <-ctx.Done(): return true
        case <-deadline.C: return true
        case <-idleTimer.C: return true
        case <-b.triggers:
            if !idleTimer.Stop() { <-idleTimer.C }
            idleTimer.Reset(b.idle)
        }
    }
}

// Drain returns and clears the pending set.
func (b *Batcher[T]) Drain() []T {
    b.mu.Lock()
    defer b.mu.Unlock()
    out := make([]T, 0, len(b.pending))
    for k := range b.pending { out = append(out, k) }
    b.pending = map[T]struct{}{}
    return out
}
```

Tests:
1. Single trigger then Wait returns true after idle elapses.
2. Multiple triggers within idle window: Wait stays blocked then returns once idle.
3. Triggers continuous past max → Wait returns at max even if idle never reached.
4. No triggers → ctx cancellation returns false.
5. Drain returns deduped UIDs.

Commit: `feat(provisioning): generic Batcher with idle/max windows`.

---

## Task 5: pod_controller.go + node_controller.go

Watch Tunnel and ExitClaim respectively. Each Reconcile is short — call `b.Trigger(uid)` and return.

```go
// pod_controller.go
type PodController struct {
    client.Client
    Batcher *Batcher[types.UID]
}
func (r *PodController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var t v1alpha1.Tunnel
    if err := r.Get(ctx, req.NamespacedName, &t); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    // Trigger only if tunnel is unscheduled or in Allocating phase.
    if t.Status.AssignedExit == "" || t.Status.Phase == v1alpha1.TunnelPhaseAllocating {
        r.Batcher.Trigger(t.UID)
    }
    return ctrl.Result{}, nil
}
func (r *PodController) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).Named("provisioning-pod").For(&v1alpha1.Tunnel{}).Complete(r)
}
```

```go
// node_controller.go
type NodeController struct {
    client.Client
    Batcher *Batcher[types.UID]
}
func (r *NodeController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var c v1alpha1.ExitClaim
    if err := r.Get(ctx, req.NamespacedName, &c); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    // Trigger when exit becomes Ready or vanishes — both unblock pending tunnels.
    r.Batcher.Trigger(c.UID)
    return ctrl.Result{}, nil
}
// SetupWithManager analogous.
```

Tests in `controllers_test.go`: envtest manager, create unscheduled Tunnel, assert `Batcher.Drain()` produces its UID within 5s.

Commit: `feat(provisioning): pod + node controllers feed the batcher`.

---

## Task 6: provisioner.go (singleton.AsReconciler)

```go
package provisioning

import (
    "context"
    "fmt"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/cloudprovider"
    "github.com/mtaku3/frp-operator/pkg/controllers/provisioning/scheduling"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

type Provisioner struct {
    Cluster       *state.Cluster
    KubeClient    client.Client
    CloudProvider *cloudprovider.Registry
    Batcher       *Batcher[types.UID]
    Scheduler     *scheduling.Scheduler
}

func New(c *state.Cluster, kube client.Client, cp *cloudprovider.Registry) *Provisioner {
    b := NewBatcher[types.UID](DefaultBatchIdleDuration, DefaultBatchMaxDuration)
    return &Provisioner{Cluster: c, KubeClient: kube, CloudProvider: cp, Batcher: b, Scheduler: scheduling.New(c, cp, kube)}
}

// Reconcile is invoked in a loop by manager since this is a singleton.
// Karpenter equivalent: pkg/controllers/provisioning/provisioner.go.
func (p *Provisioner) Reconcile(ctx context.Context) (reconcile.Result, error) {
    if !p.Batcher.Wait(ctx) { return reconcile.Result{}, nil }
    if !p.Cluster.Synced(ctx) { return reconcile.Result{RequeueAfter: 1 * time.Second}, nil }

    pending, err := p.listPendingTunnels(ctx)
    if err != nil { return reconcile.Result{}, err }
    if len(pending) == 0 { return reconcile.Result{}, nil }

    results, err := p.Scheduler.Solve(ctx, pending)
    if err != nil { return reconcile.Result{}, err }

    if err := p.persistResults(ctx, results); err != nil { return reconcile.Result{}, err }

    log.FromContext(ctx).Info("provisioning solve complete",
        "tunnels", len(pending),
        "newClaims", len(results.NewClaims),
        "bindings", len(results.Bindings),
        "errors", len(results.TunnelErrors))
    return reconcile.Result{}, nil
}

// Trigger is called by pod_controller/node_controller via Batcher.
func (p *Provisioner) Trigger(uid types.UID) { p.Batcher.Trigger(uid) }

func (p *Provisioner) listPendingTunnels(ctx context.Context) ([]*v1alpha1.Tunnel, error) {
    var list v1alpha1.TunnelList
    if err := p.KubeClient.List(ctx, &list); err != nil { return nil, err }
    out := []*v1alpha1.Tunnel{}
    for i := range list.Items {
        t := &list.Items[i]
        if t.Status.AssignedExit == "" || t.Status.Phase == v1alpha1.TunnelPhaseAllocating {
            out = append(out, t)
        }
    }
    return out, nil
}

func (p *Provisioner) persistResults(ctx context.Context, r scheduling.Results) error {
    // 1. Create each NewClaim.
    for _, c := range r.NewClaims {
        ec := &v1alpha1.ExitClaim{
            ObjectMeta: metav1.ObjectMeta{
                Name:   c.Name,
                Labels: map[string]string{v1alpha1.LabelExitPool: c.Pool.Name},
            },
            Spec: c.Spec,
        }
        if err := p.KubeClient.Create(ctx, ec); err != nil {
            if !apierrors.IsAlreadyExists(err) { return fmt.Errorf("create ExitClaim %s: %w", c.Name, err) }
            // Idempotency: if already exists, fetch and continue. The
            // deterministic naming guarantees the existing one is ours.
        }
    }
    // 2. Patch each Tunnel.Status.AssignedExit + AssignedPorts.
    for _, b := range r.Bindings {
        ns, name := splitKey(b.TunnelKey)
        var t v1alpha1.Tunnel
        if err := p.KubeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
            return fmt.Errorf("get tunnel %s: %w", b.TunnelKey, err)
        }
        patch := client.MergeFrom(t.DeepCopy())
        t.Status.AssignedExit = b.ExitClaimName
        t.Status.AssignedPorts = b.AssignedPorts
        t.Status.Phase = v1alpha1.TunnelPhaseProvisioning
        if err := p.KubeClient.Status().Patch(ctx, &t, patch); err != nil {
            return fmt.Errorf("patch tunnel %s: %w", b.TunnelKey, err)
        }
    }
    // 3. For tunnels in TunnelErrors, set Phase=Allocating + Condition Ready=False with reason.
    for key, err := range r.TunnelErrors { /* same pattern */ _ = key; _ = err }
    return nil
}

func splitKey(key string) (string, string) {
    for i := 0; i < len(key); i++ {
        if key[i] == '/' { return key[:i], key[i+1:] }
    }
    return "", key
}
```

Tests in `provisioner_test.go` (envtest):

1. Create one Tunnel + one ExitPool → Eventually one ExitClaim exists with deterministic name; Tunnel.Status.AssignedExit==<name>.
2. Create two Tunnels rapidly → after batcher window, they both bind to the SAME ExitClaim (binpack via inflight).
3. Create one Tunnel with no matching pool → Tunnel.Status.Conditions[Ready]=False with reason `NoMatchingPool`.

Commit: `feat(provisioning): singleton provisioner with Solve→Create→Patch pipeline`.

---

## Task 7: SetupWithManager wiring + suite_test.go

Provisioner needs to be registered as a singleton reconciler. controller-runtime's pattern: use `singleton.AsReconciler` from sigs.k8s.io/karpenter operator pkg, OR roll a thin wrapper:

```go
// SetupWithManager registers the Provisioner as a manager Runnable that
// loops calling Reconcile. controller-runtime doesn't natively support
// "no Request" reconcilers; we implement Runnable directly.
func (p *Provisioner) SetupWithManager(mgr ctrl.Manager) error {
    return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
        for {
            if _, err := p.Reconcile(ctx); err != nil {
                log.FromContext(ctx).Error(err, "provisioner reconcile")
            }
            if ctx.Err() != nil { return nil }
        }
    }))
}
```

Then in `suite_test.go`:
- envtest start; AddToScheme all three packages.
- Construct cluster, registry (register fake provider), provisioner.
- Wire informer controllers (state/informer) AND pod/node controllers (provisioning).
- Wire provisioner as a Runnable.
- Start manager goroutine.

Commit: `feat(provisioning): suite_test.go with full manager wiring + integration tests`.

---

## Phase 4 acceptance checklist

- [x] `pkg/controllers/provisioning/scheduling/` has pure-function predicates (requirements, ports, resources) with TDD tests.
- [x] ExistingExit, InflightClaim, NewClaimFromPool helpers + Results struct.
- [x] `Scheduler.Solve` runs three-stage pipeline; unit-tested with fake state.
- [x] Generic `Batcher[T]` with idle/max windows + tests.
- [x] PodController + NodeController feed the batcher.
- [x] Provisioner singleton runs Reconcile in a loop, calls Solve, creates ExitClaim CRs, patches Tunnel.Status.
- [x] envtest integration suite verifies binpack and limit/no-pool cases.
- [x] All `pkg/controllers/provisioning/...` tests pass.
- [x] `go vet ./...` and `go build ./...` clean.
- [x] Multiple commits.

## Out of scope (later phases)

- Lifecycle controller (Phase 5) — provider Create still doesn't run; ExitClaims sit Phase=Pending.
- Disruption (Phase 6).
- Pool counter (Phase 7) — `poolLimitsExceeded` reads from `c.Pool(name).Resources`; this is populated in Phase 7. For Phase 4 the Resources counter may be zero-valued, meaning Limits effectively don't bind yet. Acceptable; document.
- ServiceWatcher (Phase 8).
- Operator wiring (Phase 9).
- E2E (Phase 10).
