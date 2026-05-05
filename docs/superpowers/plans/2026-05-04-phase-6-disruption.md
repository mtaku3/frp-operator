# Phase 6: Disruption Controller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Build the disruption controller with pluggable Methods (Emptiness, Drift, Expiration, Consolidation), per-pool budgets that gate concurrent destructive actions, and a queue that drains+deletes candidates safely.

**Architecture:** `Controller` is a `singleton.AsReconciler` that polls every 10s. Per loop: gate on `cluster.Synced(ctx)`, build candidates from `cluster.Exits()`, ask each Method (in priority order) for commands, validate, enqueue. Methods evaluate against `state.Cluster` + `*ExitPool` budgets. Queue executes commands: mark candidate `MarkedForDeletion`, optionally launch replacements (calls Phase 4 provisioner), wait for replacements ready, then trigger ExitClaim deletion (Phase 5 finalize handles teardown).

**Spec:** §7 (Disruption controller).

**Prerequisites:** Phases 1-5 merged.

**End state:**
- `pkg/controllers/disruption/` polls cluster, decides + enqueues disruption commands, executes them via taint→wait→delete.
- Method order: Emptiness → StaticDrift → Drift → Expiration → MultiConsolidation → SingleConsolidation.
- Budgets enforced per pool per reason.
- envtest suite verifies: empty exit gets reclaimed after `ConsolidateAfter`; budget-blocked emptiness defers; expiration is forceful (bypasses budget).

---

## File map

```
pkg/controllers/disruption/
├── doc.go
├── controller.go                        # Controller + Reconcile loop, Method ordering
├── types.go                             # Method interface, Candidate, Command, DisruptionAction
├── candidates.go                        # GetCandidates(cluster), filter ShouldDisrupt, sort by cost
├── budgets.go                           # GetAllowedDisruptionsByReason(pool, reason) int
├── budgets_test.go
├── validation.go                        # post-decision re-check (do-not-disrupt, PDB-equivalent, claim still healthy)
├── queue.go                             # Queue: taint → wait for replacement Ready → delete
├── queue_test.go
├── methods/
│   ├── emptiness.go                     # IsEmpty + ConsolidateAfter elapsed
│   ├── emptiness_test.go
│   ├── drift.go                         # Pool template hash mismatch
│   ├── drift_test.go
│   ├── static_drift.go                  # Drift that needs no replacement
│   ├── expiration.go                    # ExpireAfter elapsed (forceful, bypasses budget)
│   ├── expiration_test.go
│   ├── single_consolidation.go          # Replace one exit with cheaper one (sim via provisioner.Schedule)
│   └── multi_consolidation.go
├── controller_test.go                   # envtest: empty-exit reclaim
└── suite_test.go
```

---

## Task 1: types.go — interfaces and shared structs

```go
package disruption

import (
    "context"
    "time"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Method represents one disruption strategy.
type Method interface {
    Name() string
    Reason() v1alpha1.DisruptionReason
    ShouldDisrupt(ctx context.Context, c *Candidate) bool
    ComputeCommands(ctx context.Context, budgets BudgetMap, candidates ...*Candidate) ([]*Command, error)
    Forceful() bool // true => bypasses budgets (Expiration only)
}

// Candidate is one disruptable exit.
type Candidate struct {
    Claim          *v1alpha1.ExitClaim
    State          *state.StateExit
    Pool           *v1alpha1.ExitPool
    DisruptionCost float64
    LastBindingChange time.Time
}

// Command is one decision: which candidates to disrupt and what to launch.
type Command struct {
    Candidates   []*Candidate
    Replacements []*v1alpha1.ExitClaim // empty for empty-exit deletion; populated for consolidation/drift
    Reason       v1alpha1.DisruptionReason
}

// BudgetMap is per-pool, per-reason remaining disruptions.
type BudgetMap map[string]map[v1alpha1.DisruptionReason]int

func (b BudgetMap) Allowed(poolName string, reason v1alpha1.DisruptionReason) int {
    if pm, ok := b[poolName]; ok { return pm[reason] }
    return 0
}
```

Commit: `feat(disruption): types — Method interface, Candidate, Command`.

---

## Task 2: budgets.go

```go
package disruption

import (
    "math"
    "strconv"
    "strings"
    "time"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// GetAllowedDisruptionsByReason returns the per-pool, per-reason remaining
// budget. Counts current disrupting (MarkedForDeletion) exits in each pool
// against the budget.
func GetAllowedDisruptionsByReason(cluster *state.Cluster, pool *v1alpha1.ExitPool, reason v1alpha1.DisruptionReason, now time.Time) int {
    nodesInPool := countExitsInPool(cluster, pool.Name)
    var minBudget = math.MaxInt32
    for _, b := range pool.Spec.Disruption.Budgets {
        if !budgetActive(b, reason, now) { continue }
        cap := resolveBudgetCount(b.Nodes, nodesInPool)
        if cap < minBudget { minBudget = cap }
    }
    if minBudget == math.MaxInt32 {
        // no budget set → 10% default per Karpenter
        minBudget = max(1, nodesInPool/10)
    }
    disrupting := countDisruptingInPool(cluster, pool.Name)
    return max(0, minBudget - disrupting)
}

func budgetActive(b v1alpha1.DisruptionBudget, reason v1alpha1.DisruptionReason, now time.Time) bool {
    // Reason filter: empty list = match-all; else must contain reason.
    if len(b.Reasons) > 0 {
        match := false
        for _, r := range b.Reasons { if r == reason { match = true } }
        if !match { return false }
    }
    if b.Schedule == "" { return true }
    // Cron-window check; subagent: use github.com/robfig/cron/v3 to parse Schedule + Duration.
    return cronActive(b.Schedule, *b.Duration, now)
}

func resolveBudgetCount(nodes string, total int) int {
    if strings.HasSuffix(nodes, "%") {
        pct, _ := strconv.Atoi(strings.TrimSuffix(nodes, "%"))
        return (total * pct) / 100
    }
    n, _ := strconv.Atoi(nodes)
    return n
}

func countExitsInPool(c *state.Cluster, name string) int { /* count Exits where labels[exitpool]==name */ }
func countDisruptingInPool(c *state.Cluster, name string) int { /* count MarkedForDeletion in pool */ }
func cronActive(schedule string, duration time.Duration, now time.Time) bool { /* robfig/cron NextActive */ }

func max(a, b int) int { if a > b { return a } ; return b }
```

Tests in `budgets_test.go`:
1. No budgets set → 10% default of pool size.
2. Single budget `nodes: "5"` → returns 5 minus disrupting.
3. Two budgets — min wins.
4. Reason filter matches → counted; doesn't match → ignored.
5. Cron schedule outside window → ignored.

Commit: `feat(disruption/budgets): per-pool per-reason allowed-disruption calculator`.

---

## Task 3: methods/emptiness.go (the easiest + most-used Method)

```go
package methods

import (
    "context"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

type Emptiness struct {
    Cluster   *state.Cluster
    PoolByName func(name string) *v1alpha1.ExitPool
    Now       func() time.Time
}

func (m *Emptiness) Name() string  { return "Emptiness" }
func (m *Emptiness) Reason() v1alpha1.DisruptionReason { return v1alpha1.DisruptionReasonEmpty }
func (m *Emptiness) Forceful() bool { return false }

// ShouldDisrupt: candidate is empty AND elapsed since-last-binding > ConsolidateAfter.
func (m *Emptiness) ShouldDisrupt(_ context.Context, c *disruption.Candidate) bool {
    if c.State == nil || c.Claim == nil || c.Pool == nil { return false }
    if !c.State.IsEmpty() { return false }
    consolidateAfter := c.Pool.Spec.Disruption.ConsolidateAfter.Duration
    if consolidateAfter == 0 { consolidateAfter = 5 * time.Minute }
    return m.Now().Sub(c.LastBindingChange) >= consolidateAfter
}

// ComputeCommands: take the eligible candidates, cap by budget, emit one Command per pool.
func (m *Emptiness) ComputeCommands(_ context.Context, budgets disruption.BudgetMap, candidates ...*disruption.Candidate) ([]*disruption.Command, error) {
    perPool := map[string][]*disruption.Candidate{}
    for _, c := range candidates {
        perPool[c.Pool.Name] = append(perPool[c.Pool.Name], c)
    }
    out := []*disruption.Command{}
    for poolName, cs := range perPool {
        allowed := budgets.Allowed(poolName, v1alpha1.DisruptionReasonEmpty)
        if allowed <= 0 { continue }
        if len(cs) > allowed { cs = cs[:allowed] }
        out = append(out, &disruption.Command{Candidates: cs, Reason: v1alpha1.DisruptionReasonEmpty})
    }
    return out, nil
}
```

Tests in `emptiness_test.go`:
1. Empty exit + ConsolidateAfter elapsed → ShouldDisrupt=true.
2. Empty exit + ConsolidateAfter not elapsed → false.
3. Non-empty exit → false.
4. ComputeCommands respects budget cap.

Commit: `feat(disruption/methods): Emptiness method + tests`.

---

## Task 4: methods/{drift, static_drift, expiration}.go

`drift.go` — compares `claim.Annotations[frp.operator.io/pool-hash]` (set by Phase 7 hash controller) to the current pool's hash.
`static_drift.go` — drift that doesn't require replacement (e.g. label-only changes).
`expiration.go` — `time.Since(claim.CreationTimestamp) > claim.Spec.ExpireAfter`. **Forceful=true** (bypasses budget — set `Forceful() bool = true`).

Tests for each. Commit: `feat(disruption/methods): Drift, StaticDrift, Expiration methods`.

---

## Task 5: methods/{single_consolidation, multi_consolidation}.go

The trickiest: simulate a re-bin-pack via `provisioner.Schedule`. For Phase 6, a simpler v1 implementation:

- **SingleNodeConsolidation**: pick one exit, simulate re-binding its tunnels onto OTHER existing exits (use `scheduling.Scheduler` against the cluster minus the candidate). If all tunnels fit → emit Command{candidate, no Replacements}. Else skip.
- **MultiNodeConsolidation**: pick TWO exits, simulate re-binding their tunnels onto remaining exits. If fits, emit. (Karpenter has more sophisticated logic; v1 single-pair is acceptable.)

Both gated by `Pool.Spec.Disruption.ConsolidationPolicy == "WhenEmptyOrUnderutilized"`. If policy is `WhenEmpty`, these methods short-circuit (they're for non-empty exits).

Tests with synthetic cluster:
1. Two exits, each at 50% util → multi-consolidation packs both onto one.
2. Two exits at 80% util → consolidation refused (won't fit).
3. ConsolidationPolicy=WhenEmpty → method returns no commands.

Commit: `feat(disruption/methods): single + multi consolidation via Solve simulation`.

---

## Task 6: candidates.go + validation.go + queue.go

```go
// candidates.go
func GetCandidates(cluster *state.Cluster, poolByName func(string) *v1alpha1.ExitPool, methods []Method) []*Candidate {
    var out []*Candidate
    for _, se := range cluster.Exits() {
        claim, _ := se.SnapshotForRead()
        if claim == nil { continue }
        if !isCondTrue(claim, v1alpha1.ConditionTypeReady) { continue }
        if claim.Annotations[v1alpha1.AnnotationDoNotDisrupt] != "" { continue }
        poolName := claim.Labels[v1alpha1.LabelExitPool]
        cand := &Candidate{
            Claim: claim, State: se, Pool: poolByName(poolName),
            DisruptionCost: computeCost(claim),
            LastBindingChange: lastBindingChange(claim, cluster),
        }
        out = append(out, cand)
    }
    sort.Slice(out, func(i, j int) bool { return out[i].DisruptionCost < out[j].DisruptionCost })
    return out
}

// validation.go
func Validate(ctx context.Context, cluster *state.Cluster, cmd *Command, now time.Time) error {
    // Re-check that:
    // 1. State hasn't shifted (cluster.ClusterState() before vs after).
    // 2. Each candidate still satisfies ShouldDisrupt (no longer empty etc.).
    // 3. do-not-disrupt annotation hasn't been added.
    // Returns non-nil error to abort the command.
    ...
}

// queue.go
type Queue struct {
    Client     client.Client
    Cluster    *state.Cluster
    Provisioner ProvisionerTrigger // adapter to Phase-4 provisioner
}

type ProvisionerTrigger interface {
    CreateReplacements(ctx context.Context, claims []*v1alpha1.ExitClaim) error
}

func (q *Queue) Enqueue(ctx context.Context, cmd *Command) error {
    // 1. Mark each candidate state.MarkedForDeletion = true (in-memory only).
    // 2. If cmd.Replacements not empty, launch them via provisioner.
    // 3. Wait for replacements to reach Ready (Eventually loop, ~5min ceiling).
    // 4. Drain candidates: clear bound Tunnels' AssignedExit (lifecycle.finalize handles cascade).
    // 5. client.Delete each candidate (their lifecycle finalizer takes over).
    ...
}
```

Tests for queue use envtest with fake provider; validate state transitions.

Commit: `feat(disruption): Candidate selection, post-decision validation, taint+drain queue`.

---

## Task 7: controller.go (the orchestrator)

```go
package disruption

import (
    "context"
    "time"

    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/manager"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/controllers/state"
)

const PollInterval = 10 * time.Second

type Controller struct {
    Cluster   *state.Cluster
    KubeClient client.Client
    Queue     *Queue
    Methods   []Method
    Now       func() time.Time
}

func New(c *state.Cluster, kube client.Client, q *Queue, methods []Method) *Controller {
    return &Controller{Cluster: c, KubeClient: kube, Queue: q, Methods: methods, Now: time.Now}
}

func (r *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
    if !r.Cluster.Synced(ctx) { return reconcile.Result{RequeueAfter: time.Second}, nil }

    poolByName := r.poolLookup()
    candidates := GetCandidates(r.Cluster, poolByName, r.Methods)

    for _, m := range r.Methods {
        eligible := []*Candidate{}
        for _, c := range candidates {
            if m.ShouldDisrupt(ctx, c) { eligible = append(eligible, c) }
        }
        if len(eligible) == 0 { continue }

        budgets := r.computeBudgets(poolByName, m.Reason())
        if m.Forceful() { /* bypass budgets — leave map untouched but Method should use math.MaxInt */ }

        commands, err := m.ComputeCommands(ctx, budgets, eligible...)
        if err != nil { continue }
        if len(commands) == 0 { continue }

        for _, cmd := range commands {
            if err := Validate(ctx, r.Cluster, cmd, r.Now()); err != nil { continue }
            if err := r.Queue.Enqueue(ctx, cmd); err != nil { return reconcile.Result{RequeueAfter: 30 * time.Second}, nil }
        }
        // Karpenter pattern: stop after first method that produced commands; recheck next reconcile.
        return reconcile.Result{RequeueAfter: PollInterval}, nil
    }

    return reconcile.Result{RequeueAfter: PollInterval}, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
    return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
        ticker := time.NewTicker(PollInterval); defer ticker.Stop()
        for {
            select {
            case <-ctx.Done(): return nil
            case <-ticker.C:
                if _, err := r.Reconcile(ctx); err != nil { /* log */ }
            }
        }
    }))
}

func (r *Controller) computeBudgets(poolByName func(string) *v1alpha1.ExitPool, reason v1alpha1.DisruptionReason) BudgetMap { /* ... */ }
func (r *Controller) poolLookup() func(string) *v1alpha1.ExitPool { /* ... */ }
```

envtest in `controller_test.go`:
1. Empty exit + 5min wait → exit gets deleted; provider.DeleteCallCount == 1.
2. Empty exit + budget=0 → no delete after several poll intervals.
3. Expired exit + budget=0 → still deleted (Forceful bypasses budget).
4. Exit with bound tunnel → never selected as Empty candidate.

Commit: `feat(disruption): controller orchestrating Methods + Queue`.

---

## Phase 6 acceptance checklist

- [x] Method interface with five impls: Emptiness, Drift, StaticDrift, Expiration, MultiConsolidation, SingleConsolidation.
- [x] Budgets: per-pool, per-reason, with cron-window support and `nodes: "10%"` percentage syntax.
- [x] Forceful() bypasses budget (Expiration only).
- [x] Queue: taint → wait for replacements (if any) → drain bindings → delete claim.
- [x] envtest verifies happy paths and budget gating.
- [x] All tests pass.

## Out of scope

- Pool counter / hash (Phase 7) — Phase 6 reads `claim.Annotations[pool-hash]` if present; if Phase 7 hasn't shipped, drift methods short-circuit.
- ServiceWatcher (Phase 8).
- Operator wiring (Phase 9).

## Coordination notes

- Phase 7 stamps `claim.Annotations[frp.operator.io/pool-hash]` AND `pool.Annotations[frp.operator.io/pool-hash]`. Drift method compares the two. If Phase 7 hasn't landed, the annotation is empty and drift never fires — that's safe.
- The `LastBindingChange` field on Candidate is sourced from a per-claim metric tracked by `state.Cluster` whenever a binding changes. If state.Cluster doesn't yet record it, derive from `claim.CreationTimestamp` as a worst-case fallback (will fire on every empty exit immediately after `ConsolidateAfter`, conservatively correct).
