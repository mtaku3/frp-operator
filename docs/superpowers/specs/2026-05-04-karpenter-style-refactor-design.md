# Karpenter-Style Refactor — Design Spec

**Status:** Draft
**Date:** 2026-05-04
**Scope:** Big-bang rewrite of internal/controller, internal/scheduler, internal/provider boundaries, and CRD set. Mirrors `sigs.k8s.io/karpenter` core architecture.

---

## 1. Motivation

Current architecture (v1alpha1: `Tunnel`, `ExitServer`, `SchedulingPolicy`) places the scheduler inline in the Tunnel controller. Each Tunnel reconcile lists ExitServers, runs allocator, and on miss creates an ExitServer with `GenerateName`. Race conditions during the create→status-patch window cause duplicate ExitServers (PR #5 patched this with a label breadcrumb but the underlying design is fragile).

Pain points:

- **Scheduler is not a singleton.** Concurrent Tunnel reconciles each independently call allocator → independently provision → bin-packing breaks under burst.
- **No batching.** A burst of N Tunnels triggers N independent reconciles racing for capacity.
- **No state cache.** Every reconcile re-lists from APIReader (informer-bypass), wasting calls and still missing in-flight provisions.
- **No clean lifecycle CRD.** ExitServer is both the "claim" and the "runtime state" — mixing scheduler intent with provider truth makes drift detection awkward.
- **Provider config muddled into ExitServer spec.** `Provider`, `CredentialsRef`, region/size all live on the per-instance CR. No reusable provider profile.
- **Reclaim controller couples emptiness, drift, expiry into one state machine.** Hard to extend (add cost-based consolidation, replacement scheduling, budget caps).
- **No declarative budgets.** `SchedulingPolicy.Spec.Budget` exists for max-exits but nothing for bandwidth, cost, or per-reason disruption rate.
- **Webhook validation surface tangled with controller logic.**

Karpenter solves these via:
- Singleton provisioner driven by trigger-batcher.
- Shared in-memory `state.Cluster` cache, gated by `Synced(ctx)` before every decision.
- Three CRDs split by concern: `NodePool` (template), `NodeClaim` (lifecycle), `NodeClass` (provider config).
- Lifecycle controller with discrete phases (Launch → Register → Initialize → Live).
- Disruption controller with pluggable Methods, all gated by per-pool budgets.
- No admission webhooks (CEL + status validation only).

This spec defines the equivalent for our domain.

---

## 2. Resource model

Three core CRDs + one CRD per cloud provider.

| Karpenter | Ours | Role | Scope |
|-----------|------|------|-------|
| `NodePool` | `ExitPool` | template + scheduling constraints + disruption policy | cluster |
| `NodeClaim` | `ExitClaim` | per-instance lifecycle unit | cluster |
| `NodeClass` (per provider) | `ProviderClass` (per provider, e.g. `LocalDockerProviderClass`, `DigitalOceanProviderClass`) | provider-specific config | cluster |
| `Pod` (k8s native) | `Tunnel` | consumer | namespaced |
| `Node` (k8s native) | (no CR) | runtime state on provider | n/a — tracked via `ExitClaim.Status.ProviderID` |

**Why no Exit CR:** Karpenter only has `Node` because kubelet creates it natively. There is no native equivalent for an frps process; runtime state lives on `ExitClaim.Status`. Adding an `Exit` CR would duplicate `ExitClaim.Status` data without adding information.

Group: `frp.operator.io`. API version: `v1alpha1` initially, with conversion webhook scaffolding from day 1.

### Key relationships

```
Tunnel  --(Status.AssignedExit)-->  ExitClaim
                                        |
                                        +--(Spec.ProviderClassRef)--> ProviderClass
                                        |
                                        +--(label frp.operator.io/exitpool)--> ExitPool
```

- **No OwnerReferences from Tunnel → ExitClaim.** ExitClaims outlive Tunnels (binpack: tunnel A creates exit, tunnel A deleted, exit kept for tunnel B). Cascade-delete would be wrong.
- **No OwnerReferences from ExitPool → ExitClaim.** Pool is template only. Link is the label `frp.operator.io/exitpool=<pool-name>` on each ExitClaim.
- **Single finalizer**: `frp.operator.io/termination` on ExitClaim. Drives drain + provider delete.

---

## 3. CRD specifications

### 3.1 ExitPool

```go
type ExitPoolSpec struct {
    Template     ExitClaimTemplate `json:"template"`
    Disruption   Disruption        `json:"disruption,omitempty"`
    Limits       *Limits           `json:"limits,omitempty"`
    Weight       *int32            `json:"weight,omitempty"`       // 0-100, higher wins on tie
    Replicas     *int64            `json:"replicas,omitempty"`     // alpha: static mode (feature-gated)
}

type ExitClaimTemplate struct {
    Metadata struct {
        Labels      map[string]string `json:"labels,omitempty"`
        Annotations map[string]string `json:"annotations,omitempty"`
    } `json:"metadata,omitempty"`
    Spec ExitClaimTemplateSpec `json:"spec"`
}

type ExitClaimTemplateSpec struct {
    ProviderClassRef        ProviderClassRef                          `json:"providerClassRef"`
    Requirements            []NodeSelectorRequirementWithMinValues    `json:"requirements,omitempty"`
    Frps                    FrpsConfig                                `json:"frps"`
    Capacity                *ExitCapacity                             `json:"capacity,omitempty"`
    ExpireAfter             Duration                                  `json:"expireAfter,omitempty"`
    TerminationGracePeriod  *Duration                                 `json:"terminationGracePeriod,omitempty"`
}

type FrpsConfig struct {
    Version          string         `json:"version"`                    // e.g. "v0.68.1"
    BindPort         int32          `json:"bindPort"`                   // control plane (frpc connects here), default 7000
    AdminPort        int32          `json:"adminPort,omitempty"`        // admin API port, default 7400
    VhostHTTPPort    *int32         `json:"vhostHTTPPort,omitempty"`    // optional
    VhostHTTPSPort   *int32         `json:"vhostHTTPSPort,omitempty"`   // optional
    KCPBindPort      *int32         `json:"kcpBindPort,omitempty"`      // optional
    QUICBindPort     *int32         `json:"quicBindPort,omitempty"`     // optional
    AllowPorts       []string       `json:"allowPorts"`                 // public port slots, e.g. ["80","443","1024-65535"]
    ReservedPorts    []int32        `json:"reservedPorts,omitempty"`    // internal/admin ports excluded from allocation
    Auth             FrpsAuthConfig `json:"auth"`
    TLS              *FrpsTLSConfig `json:"tls,omitempty"`
}

type FrpsAuthConfig struct {
    Method            string             `json:"method"`            // "token" only for v1
    TokenSecretRef    *SecretKeyRef      `json:"tokenSecretRef,omitempty"`  // generated by operator if unset
    OIDC              *FrpsOIDCConfig    `json:"oidc,omitempty"`            // future
}

type FrpsTLSConfig struct {
    Force      bool          `json:"force,omitempty"`              // require TLS on control plane
    CertSecret *SecretKeyRef `json:"certSecret,omitempty"`
    KeySecret  *SecretKeyRef `json:"keySecret,omitempty"`
    CASecret   *SecretKeyRef `json:"caSecret,omitempty"`           // for mTLS
}

type ProviderClassRef struct {
    Group string `json:"group"`     // e.g. frp.operator.io
    Kind  string `json:"kind"`      // e.g. LocalDockerProviderClass
    Name  string `json:"name"`
}

type NodeSelectorRequirementWithMinValues struct {
    Key       string                  `json:"key"`
    Operator  NodeSelectorOperator    `json:"operator"`        // In, NotIn, Exists, DoesNotExist, Gt, Lt
    Values    []string                `json:"values,omitempty"`
    MinValues *int                    `json:"minValues,omitempty"`
}

type Disruption struct {
    ConsolidationPolicy ConsolidationPolicy `json:"consolidationPolicy,omitempty"` // WhenEmpty | WhenEmptyOrUnderutilized
    ConsolidateAfter    Duration            `json:"consolidateAfter,omitempty"`
    Budgets             []DisruptionBudget  `json:"budgets,omitempty"`
}

type DisruptionBudget struct {
    Nodes    string             `json:"nodes"`               // "10%" or "5"
    Schedule string             `json:"schedule,omitempty"`  // crontab syntax
    Duration *Duration          `json:"duration,omitempty"`
    Reasons  []DisruptionReason `json:"reasons,omitempty"`   // Drifted, Empty, Expired, Underutilized
}

type Limits struct {
    Exits             *int64    `json:"exits,omitempty"`
    BandwidthMbps     *int64    `json:"bandwidthMbps,omitempty"`
    MonthlyTrafficGB  *int64    `json:"monthlyTrafficGB,omitempty"`
    // MonthlyCostCents deferred to v1beta1 — requires per-provider Pricing
    // controller (Karpenter has one for AWS pulling spot/on-demand prices).
    // localdocker = 0; DO = static table or API. Add when Pricing CRD lands.
}

type ExitPoolStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"` // Ready, ProviderClassReady, ValidationSucceeded
    Exits      int64              `json:"exits"`
    Resources  ResourceUsage      `json:"resources,omitempty"`
}
```

**Defaulting:**
- `ExpireAfter`: 720h (30d).
- `Disruption.ConsolidationPolicy`: `WhenEmpty`.
- `Disruption.ConsolidateAfter`: 5m.
- Budget if none specified: `[{nodes: "10%"}]`.

**CEL validation:**
- `requirements[].operator in {In, NotIn, Exists, DoesNotExist, Gt, Lt}`.
- `requirements[].operator == 'Gt' || operator == 'Lt'` ⇒ `len(values) == 1` and value parses as int.
- `weight` in `[0,100]`.
- `replicas` set ⇒ `weight` unset.
- `limits.exits >= 0`.

### 3.2 ExitClaim

```go
type ExitClaimSpec struct {
    ProviderClassRef        ProviderClassRef                       `json:"providerClassRef"`
    Requirements            []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
    Frps                    FrpsConfig                             `json:"frps"`
    Resources               ResourceRequirements                   `json:"resources,omitempty"`
    Capacity                *ExitCapacity                          `json:"capacity,omitempty"`
    ExpireAfter             Duration                               `json:"expireAfter,omitempty"`
    TerminationGracePeriod  *Duration                              `json:"terminationGracePeriod,omitempty"`
}

type ResourceRequirements struct {
    Requests map[string]resource.Quantity `json:"requests,omitempty"`
    // dimensions: portSlots, bandwidthMbps, monthlyTrafficGB
    // NOT cpu/memory — our binpack key is port slots, not VPS resources.
    // VPS cpu/mem live on InstanceType.Capacity (informational only).
}

type ExitClaimStatus struct {
    ProviderID       string             `json:"providerID,omitempty"`
    PublicIP         string             `json:"publicIP,omitempty"`
    ExitName         string             `json:"exitName,omitempty"`         // provider-side identifier (container name, droplet name)
    ProviderImageID  string             `json:"providerImageID,omitempty"`  // localdocker: image digest; DO: droplet image ID
    FrpsVersion      string             `json:"frpsVersion,omitempty"`      // actually-running version reported by admin API
    Capacity         map[string]string  `json:"capacity,omitempty"`
    Allocatable      map[string]string  `json:"allocatable,omitempty"`
    Allocations      map[string]string  `json:"allocations,omitempty"`      // port → "<ns>/<tunnel>"
    HourlyCostCents  *int64             `json:"hourlyCostCents,omitempty"`  // stamped at launch from InstanceType.Offerings (deferred to v1beta1)
    Conditions       []metav1.Condition `json:"conditions,omitempty"`
}
```

**Conditions** (mirroring NodeClaim):

| Type | Meaning |
|------|---------|
| `Launched` | provider `Create` returned successfully; `ProviderID` populated |
| `Registered` | frps admin API reachable, control plane joined |
| `Initialized` | reservePorts ran, ready for tunnels |
| `Ready` | composite of above |
| `Drifted` | spec hash mismatch with parent ExitPool |
| `Empty` | `len(Allocations) == 0` and idle past `ConsolidateAfter` |
| `Consolidatable` | candidate for cost-based consolidation |
| `Disrupted` | scheduled for replacement / deletion |
| `Expired` | `ExpireAfter` elapsed since creation |
| `ConsistentStateFound` | provider state matches spec (drift detection) |

**Lifecycle**: `Created → Launched → Registered → Initialized → Ready → (Drifted | Empty | Expired | Consolidatable) → Disrupted → terminated`.

**Finalizer**: `frp.operator.io/termination`. Removed by termination controller after drain + `cloudProvider.Delete`.

### 3.3 ProviderClass (per-provider CRD)

Each provider ships its own. Examples:

```yaml
apiVersion: frp.operator.io/v1alpha1
kind: LocalDockerProviderClass
metadata: {name: default}
spec:
  network: kind
  configHostMountPath: /tmp/frp-operator-shared
  imagePullPolicy: IfNotPresent
  skipHostPortPublishing: true
  defaultImage: fatedier/frps:v0.68.1
status:
  conditions: [{type: Ready, status: "True"}]
```

```yaml
apiVersion: frp.operator.io/v1alpha1
kind: DigitalOceanProviderClass
metadata: {name: default}
spec:
  apiTokenSecretRef: {name: do-token, key: token}
  region: nyc3
  size: s-1vcpu-1gb
  imageID: ubuntu-22-04-x64
  vpcUUID: ""
  sshKeyIDs: []
  monitoring: true
  defaultImage: fatedier/frps:v0.68.1
status:
  conditions: [{type: Ready, status: "True"}]
```

Cluster-scoped. One ProviderClass type per provider package. Discovery via `CloudProvider.GetSupportedProviderClasses() []status.Object`.

### 3.4 Tunnel (mostly unchanged)

```go
type TunnelSpec struct {
    Ports                     []TunnelPort                           `json:"ports"`
    Requirements              []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
    TopologySpreadConstraints []TopologySpread                       `json:"topologySpreadConstraints,omitempty"` // future
    ExitClaimRef              *LocalObjectReference                  `json:"exitClaimRef,omitempty"`              // hard pin
    Resources                 *TunnelResources                       `json:"resources,omitempty"`                 // bandwidth, traffic
}

type TunnelResources struct {
    BandwidthMbps    *int32 `json:"bandwidthMbps,omitempty"`
    MonthlyTrafficGB *int64 `json:"monthlyTrafficGB,omitempty"`
}

type TunnelStatus struct {
    Phase           TunnelPhase        `json:"phase,omitempty"`
    AssignedExit    string             `json:"assignedExit,omitempty"`
    AssignedIP      string             `json:"assignedIP,omitempty"`
    AssignedPorts   []int32            `json:"assignedPorts,omitempty"`
    Conditions      []metav1.Condition `json:"conditions,omitempty"`
}
```

**Annotations honored:**
- `frp.operator.io/do-not-disrupt: "true"` — block reclaim while tunnel alive.
- `frp.operator.io/do-not-disrupt: "30m"` — duration form.

**ServiceWatcher** stays unchanged — creates Tunnel from `Service.spec.loadBalancerClass=frp-operator.io/frp`.

### 3.5 Well-known labels

| Label | Set on | Source |
|-------|--------|--------|
| `frp.operator.io/exitpool` | ExitClaim | scheduler at create |
| `frp.operator.io/region` | ExitClaim | resolved from ProviderClass + Pool requirements |
| `frp.operator.io/provider` | ExitClaim | from ProviderClass kind |
| `frp.operator.io/tier` | ExitClaim | from Pool template label |
| `frp.operator.io/created-for-tunnel` | ExitClaim (when applicable) | scheduler if tunnel pinned exit |

These are the keys `requirements` operate on.

---

## 4. Controller layout

All controllers run in one `manager.Manager`, share one `*state.Cluster`. Wired in `pkg/controllers/controllers.go:NewControllers`.

```
pkg/controllers/
├── controllers.go                       # NewControllers, manager wiring
├── provisioning/
│   ├── provisioner.go                   # singleton, Reconcile(ctx)
│   ├── batcher.go                       # Trigger/Wait, idle 1s max 10s
│   ├── pod_controller.go                # watches Tunnels, calls Trigger
│   ├── node_controller.go               # watches ExitClaims, calls Trigger
│   └── scheduling/
│       ├── scheduler.go                 # Solve(ctx, tunnels)
│       ├── existing_exit.go             # CanAdd for Ready exits
│       ├── inflight_claim.go            # CanAdd for speculative claims
│       ├── new_claim.go                 # build new ExitClaim from pool template
│       ├── topology.go                  # spread constraints (future)
│       ├── requirements.go              # NodeSelectorRequirementWithMinValues
│       └── results.go                   # Results{NewClaims, ExistingExits, TunnelErrors}
├── disruption/
│   ├── controller.go                    # singleton, picks Method per loop
│   ├── methods.go                       # Method interface
│   ├── emptiness.go
│   ├── drift.go
│   ├── expiration.go
│   ├── multi_consolidation.go
│   ├── single_consolidation.go
│   ├── budgets.go                       # GetAllowedDisruptionsByReason
│   ├── validation.go                    # post-decision re-check
│   └── queue.go                         # taint → wait → delete pipeline
├── exitclaim/
│   ├── lifecycle/
│   │   ├── controller.go                # fans out to launch → register → init → liveness
│   │   ├── launch.go                    # cloudProvider.Create
│   │   ├── registration.go              # admin API probe
│   │   ├── initialization.go            # reservePorts, mark Ready
│   │   └── liveness.go                  # RegistrationTTL, mark for delete on stale
│   ├── disruption/
│   │   └── controller.go                # sets Drifted/Empty/Consolidatable on status
│   ├── garbagecollection/
│   │   └── controller.go                # cloud List vs API list, delete orphans
│   ├── expiration/
│   │   └── controller.go                # requeue at ExpireAfter, mark Disrupted
│   ├── consistency/
│   │   └── controller.go                # capacity vs allocations anomaly detection
│   └── tunnelevents/
│       └── controller.go                # last-tunnel-bound timestamp for emptiness TTL
├── exit/
│   └── termination/
│       └── controller.go                # drain tunnels, cloudProvider.Delete, strip finalizer
├── exitpool/
│   ├── counter/
│   │   └── controller.go                # roll up Status.Exits / Resources
│   ├── hash/
│   │   └── controller.go                # stamp pool template hash for drift detection
│   ├── readiness/
│   │   └── controller.go                # ProviderClassReady → Pool.Ready
│   └── validation/
│       └── controller.go                # ValidationSucceeded condition
├── state/
│   ├── cluster.go                       # *state.Cluster
│   ├── stateexit.go                     # joins ExitClaim + provider Exit data
│   └── informer/
│       ├── exitclaim_controller.go      # watch → update cluster
│       ├── exitpool_controller.go
│       ├── tunnel_controller.go
│       ├── providerclass_controller.go
│       └── pricing_controller.go        # if we add cost
└── metrics/
    ├── exit_metrics.go
    ├── exitpool_metrics.go
    └── tunnel_metrics.go
```

### Coordination model

Controllers do **not** call each other. They:
1. Watch their own resource type, write status.
2. Read shared `*state.Cluster` for cross-resource data.
3. Gate every decision on `cluster.Synced(ctx) == true`.

The provisioner and disruption controllers are `singleton.AsReconciler` — no Request, manager calls `Reconcile(ctx)` in a loop.

---

## 5. Provisioning loop

### Entry: `provisioner.go`

```go
type Provisioner struct {
    kubeClient   client.Client
    cluster      *state.Cluster
    cloudProvider cloudprovider.CloudProvider
    batcher      *Batcher[types.UID]
}

func (p *Provisioner) Reconcile(ctx context.Context) (reconcile.Result, error)
func (p *Provisioner) Trigger(uid types.UID)
func (p *Provisioner) Schedule(ctx context.Context) (scheduling.Results, error)
func (p *Provisioner) CreateExitClaims(ctx, []*scheduling.ExitClaim, ...opts) ([]string, error)
```

Reconcile pseudocode:
```
loop {
    triggered := batcher.Wait(ctx)        // blocks for first trigger, then idle 1s, max 10s
    if !triggered { return }
    if !cluster.Synced(ctx) { requeue 1s; return }

    pendingTunnels := list Tunnels where Status.AssignedExit == "" or assigned exit is Lost/Disrupted

    results, err := scheduler.Solve(ctx, pendingTunnels)

    for each newClaim in results.NewClaims:
        create ExitClaim API object (deterministic name from pool + UID hash)

    for each (tunnel, exit) in results.Bindings:
        patch tunnel.Status.AssignedExit, exit.Status.Allocations[port] = tunnel-key
}
```

### Batching

`Batcher[T comparable]`:
```go
type Batcher[T comparable] struct {
    idleDuration time.Duration   // BATCH_IDLE_DURATION, default 1s
    maxDuration  time.Duration   // BATCH_MAX_DURATION, default 10s
    triggers     chan T
    pending      sync.Map
}

func (b *Batcher[T]) Trigger(t T)
func (b *Batcher[T]) Wait(ctx context.Context) bool
```

`Trigger` is called by:
- `pod_controller.go` (Tunnel watcher) when Tunnel is created or returns to unscheduled state.
- `node_controller.go` (ExitClaim watcher) when an ExitClaim becomes Ready, Lost, or Empty.

`Wait` blocks until first trigger, then resets idle timer on each subsequent trigger up to `maxDuration` ceiling. Returns when no trigger seen for `idleDuration` or `maxDuration` reached.

### Solve pipeline

`scheduling/scheduler.go`:

```go
func (s *Scheduler) Solve(ctx, tunnels []*Tunnel) (Results, error) {
    queue := buildPriorityQueue(tunnels)
    for queue.NotEmpty() {
        t := queue.Pop()
        if err := s.add(ctx, t); err != nil {
            if isPreferenceFailure(err) {
                if relaxed := s.preferences.Relax(t); relaxed {
                    queue.PushBack(t)
                    continue
                }
            }
            results.TunnelErrors[t] = err
        }
    }
    return results, nil
}

func (s *Scheduler) add(ctx, t *Tunnel) error {
    if err := s.addToExistingExit(ctx, t); err == nil { return nil }
    if err := s.addToInflightClaim(ctx, t); err == nil { return nil }
    return s.addToNewClaim(ctx, t)
}
```

**`addToExistingExit`** — iterate `s.existingExits` (`*ExistingExit` wrapping `*state.StateExit`), parallelize candidate evaluation, take earliest-indexed success. `CanAdd(t)` checks: Phase=Ready, Pool requirements satisfied, port slots available, capacity available, do-not-disrupt-OK.

**`addToInflightClaim`** — iterate `s.newClaims` (claims this Solve already decided to create). Pre-sorted ascending by `len(Tunnels)` so lightly-loaded packed first. Reuses an in-flight claim's reserved AllowPorts.

**`addToNewClaim`** — iterate `s.exitClaimTemplates` (one per managed ExitPool, sorted by `Spec.Weight` desc). Build fresh `ExitClaim` from template, run `CanAdd`, apply Pool `Limits`. On success, append to `s.newClaims`.

### In-flight tracking

Per-Solve, in-memory only. Lives on `Scheduler.newClaims` (slice of `*scheduling.ExitClaim`). Subsequent tunnels in the same Solve can pack onto these speculative claims via `addToInflightClaim`. Persisted only when scheduler writes the API object at the end.

**Important:** `state.Cluster` does NOT track in-flight claims from a different Solve run. Each Solve starts fresh. This is fine because:
1. Provisioner is a singleton (no concurrent Solves).
2. ExitClaim API objects created at the end of a Solve become `state.Cluster` entries on the next informer event.

### Results type

```go
type Results struct {
    NewClaims      []*ExitClaim                  // to be created
    ExistingExits  []*ExistingExit               // bindings to existing
    TunnelErrors   map[*Tunnel]error             // unscheduled with reason
}

func (r Results) Record(events recorder.EventRecorder)
func (r Results) AllScheduled() bool
```

---

## 6. ExitClaim lifecycle controller

`pkg/controllers/exitclaim/lifecycle/controller.go`. Single controller, four sequential phases per Reconcile.

```go
func (c *Controller) Reconcile(ctx, req) (Result, error) {
    var claim ExitClaim
    if err := c.Get(ctx, req.NamespacedName, &claim); err != nil { ... }

    if !claim.DeletionTimestamp.IsZero() {
        return c.finalize(ctx, &claim)
    }

    if added := addFinalizer(&claim); added {
        return Requeue, c.Update(ctx, &claim)
    }

    for _, phase := range []phaseFn{
        c.launch.Reconcile,
        c.registration.Reconcile,
        c.initialization.Reconcile,
        c.liveness.Reconcile,
    } {
        if res, err := phase(ctx, &claim); err != nil || !res.IsZero() {
            return res, err
        }
    }
    return ctrl.Result{}, nil
}
```

### launch.go

If `Conditions[Launched].Status == Unknown`:
1. Resolve `ProviderClassRef` to a concrete provider config object.
2. Resolve credentials from `ProviderClass.Spec.*SecretRef` (ours: `apiTokenSecretRef`, etc).
3. Call `cloudProvider.Create(ctx, &claim)`.
4. Returned hydrated claim has `Status.ProviderID`, `Status.ExitName`, `Status.Capacity`, `Status.Allocatable`, `Status.PublicIP` (when known).
5. Patch claim status. Set `Conditions[Launched]=True`.
6. Cache the result for 1h to survive informer staleness on next reconcile (Karpenter does this).

**Idempotency**: deterministic claim name (`<pool>-<8-char-hash-of-claim-uid>`) means provider's `Create` can dedupe internally if it sees the same name. For localdocker: container name is `frp-operator-<ns>__<claim-name>`. Provider must handle "container with this name already exists" → fetch and return its info.

### registration.go

If `Launched=True` but `Registered≠True`:
1. Call `cloudProvider.Get(ctx, providerID)` to confirm provider-side liveness.
2. Probe admin API: `adminClient.GetServerInfo()`. Expect 200 within `RegistrationTTL` (15m).
3. Run any registered `cloudprovider.NodeLifecycleHook.Registered(ctx, claim)`.
4. Patch `Conditions[Registered]=True`.

### initialization.go

If `Registered=True` but `Initialized≠True`:
1. Reserve internal ports (admin, control) on the exit.
2. Persist to `Spec.AllowPorts \ ReservedPorts` view.
3. Patch `Conditions[Initialized]=True` and `Conditions[Ready]=True`.

### liveness.go

If `Launched=True` but `Registered≠True` for `> RegistrationTTL`:
- Mark `Conditions[Disrupted]=True, Reason=RegistrationTimeout`.
- Trigger termination by calling `c.Delete(ctx, &claim)`.

### finalize() — termination path

Triggered by `claim.DeletionTimestamp != nil` and finalizer present:
1. Wait for `len(claim.Status.Allocations) == 0` OR `TerminationGracePeriod` elapsed.
2. Drain: notify each tunnel in allocations to release (`Tunnel.Status.AssignedExit = ""`).
3. Call `cloudProvider.Delete(ctx, &claim)`. Provider returns `NotFoundError` once gone.
4. Strip finalizer.

---

## 7. Disruption controller

`pkg/controllers/disruption/controller.go`. Singleton, polls every 10s.

### Method order

```go
func (c *Controller) Methods() []Method {
    return []Method{
        NewEmptiness(c),
        NewStaticDrift(...),
        NewDrift(...),
        NewExpiration(...),
        NewMultiExitConsolidation(c),
        NewSingleExitConsolidation(c),
    }
}
```

Per loop, controller:
1. Calls `cluster.Synced(ctx)`.
2. Builds `[]*Candidate` from `cluster.Exits()`.
3. For each Method in order, calls `ComputeCommands(budgets, candidates)`. Stops at first non-empty.
4. Validates command (re-check state, PDBs, do-not-disrupt).
5. Enqueues to `disruption.Queue`.

### Method interface

```go
type Method interface {
    ShouldDisrupt(ctx context.Context, c *Candidate) bool
    ComputeCommands(ctx context.Context, budgets BudgetMap, candidates ...*Candidate) ([]Command, error)
    Reason() DisruptionReason
}

type Candidate struct {
    Claim       *ExitClaim
    StateExit   *state.StateExit
    Pool        *ExitPool
    DisruptionCost float64
}

type Command struct {
    Candidates []*Candidate
    Replacements []*scheduling.ExitClaim   // if consolidation requires replacement
    Reason DisruptionReason
}
```

### Methods

**Emptiness** — `Status.Allocations` empty AND `Conditions[Empty]=True for > ConsolidateAfter`. Cheapest.

**StaticDrift** — pool template hash mismatch, no replacement needed (e.g. only label change).

**Drift** — pool template hash mismatch requiring replacement. Computes via `provisioner.Schedule()` simulation with current allocations re-assigned.

**Expiration** — `ExpireAfter` elapsed. **Forceful**: bypasses budget. Always replaces if tunnels still bound.

**MultiExitConsolidation / SingleExitConsolidation** — re-binpack to fewer/cheaper exits. Simulation via scheduler with candidate's tunnels removed; if simulation produces lower cost, command is "delete candidate, schedule replacements."

### Budget enforcement

`budgets.go:GetAllowedDisruptionsByReason(ctx, pool, reason) int`:
- Read `Pool.Spec.Disruption.Budgets`.
- For each budget, evaluate `Schedule`/`Duration` to see if active.
- Count active budgets, take min `Nodes` (resolved % vs absolute).
- Subtract currently-disrupting count from `state.Cluster`.
- Return remainder.

If 0, Method skips action.

### Queue execution

`disruption.Queue` runs:
1. Apply taint `frp.operator.io/disruption:NoSchedule` to candidate.
2. Block scheduler from binding new tunnels (already filtered via `state.StateExit.MarkedForDeletion`).
3. If replacements needed, call `provisioner.CreateExitClaims(replacements)`.
4. Wait for replacements to reach `Conditions[Initialized]=True`.
5. For each candidate: trigger drain + Delete via lifecycle controller.

---

## 8. State.Cluster

`pkg/controllers/state/cluster.go`.

```go
type Cluster struct {
    mu                       sync.RWMutex
    exits                    map[string]*StateExit       // keyed by ProviderID
    bindings                 map[NamespacedName]string   // tunnel → ExitClaim name
    tunnelToClaim            sync.Map
    poolResources            map[string]ResourceUsage
    daemonSetTunnels         sync.Map                    // not applicable, omit
    clusterState             time.Time                   // monotonic version stamp
    nameToProviderID         map[string]string
    triggerProvisioner       func()
    triggerDisruption        func()
}

type StateExit struct {
    Claim                *v1alpha1.ExitClaim
    ProviderState        *cloudprovider.ProviderState   // last cloud Get result
    Allocations          map[string]TunnelKey            // port → tunnel
    Bandwidth            int64                           // current usage
    MarkedForDeletion    bool
    Nominated            bool                            // tagged by provisioner this Solve
    DisruptionCost       float64
}

func (c *Cluster) Synced(ctx context.Context) bool       // do list-vs-cache reconcile
func (c *Cluster) Exits() []*StateExit
func (c *Cluster) ExitForProviderID(id string) *StateExit
func (c *Cluster) ExitForTunnel(t NamespacedName) *StateExit
func (c *Cluster) UpdateExit(claim *v1alpha1.ExitClaim)
func (c *Cluster) DeleteExit(name string)
func (c *Cluster) UpdateTunnelBinding(t NamespacedName, claimName string)
```

### Sync model

`state/informer/*` controllers are write-only. Each watches its resource type and updates `state.Cluster`:
- `exitclaim_controller.go` → `cluster.UpdateExit`.
- `exitpool_controller.go` → `cluster.UpdatePool`.
- `tunnel_controller.go` → `cluster.UpdateTunnelBinding`.
- `providerclass_controller.go` → invalidate cached templates.

Read-only consumers (provisioner, disruption) gate on `cluster.Synced(ctx)` which performs an internal list-vs-cache reconcile to ensure no informer lag.

### In-flight tracking

NOT in `state.Cluster`. Lives in `Scheduler.newClaims` per-Solve.

---

## 9. CloudProvider interface

`pkg/cloudprovider/types.go`:

```go
type CloudProvider interface {
    // Create launches a new exit. Returns a hydrated ExitClaim with
    // Status.ProviderID, ExitName, Capacity, Allocatable, PublicIP set.
    Create(context.Context, *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error)

    // Delete the exit. Returns NewExitNotFoundError once gone.
    Delete(context.Context, *v1alpha1.ExitClaim) error

    // Get returns the live state of the exit by ProviderID.
    Get(context.Context, providerID string) (*v1alpha1.ExitClaim, error)

    // List enumerates all exits the provider knows about (for GC).
    List(context.Context) ([]*v1alpha1.ExitClaim, error)

    // GetInstanceTypes returns provisionable instance shapes for a pool.
    GetInstanceTypes(context.Context, *v1alpha1.ExitPool) ([]*InstanceType, error)

    // IsDrifted compares provider state against claim spec.
    IsDrifted(context.Context, *v1alpha1.ExitClaim) (DriftReason, error)

    // RepairPolicies declares which conditions the provider can repair.
    RepairPolicies() []RepairPolicy

    // Name identifies the provider.
    Name() string

    // GetSupportedProviderClasses returns the CRD types this provider accepts.
    GetSupportedProviderClasses() []status.Object
}

type InstanceType struct {
    Name         string
    Requirements scheduling.Requirements
    Offerings    Offerings
    Capacity     ResourceList
    Overhead     ResourceList
}

type Offering struct {
    Requirements scheduling.Requirements    // zone, capacity-type, etc.
    Price        float64
    Available    bool
}
```

Optional `NodeLifecycleHook`:
```go
type NodeLifecycleHook interface {
    Registered(ctx context.Context, claim *v1alpha1.ExitClaim) (NodeLifecycleHookResult, error)
}
```

### Provider implementations

`pkg/cloudprovider/localdocker/` — wraps current `internal/provider/localdocker` logic.
`pkg/cloudprovider/digitalocean/` — wraps current `internal/provider/digitalocean`.
`pkg/cloudprovider/fake/` — full in-memory impl for tests.

Provider-specific config CRDs live under each provider's package (e.g. `pkg/cloudprovider/localdocker/v1alpha1/localdockerproviderclass_types.go`).

---

## 10. Operator wiring

`pkg/operator/operator.go`:

```go
type Operator struct {
    *manager.Manager
    Cluster        *state.Cluster
    CloudProvider  cloudprovider.CloudProvider
    EventRecorder  record.EventRecorder
}

func NewOperator(ctx, cfg) (*Operator, error) {
    mgr := ctrl.NewManager(cfg, ctrl.Options{
        Scheme: scheme,
        LeaderElection:                 !cfg.DisableLeaderElection,
        LeaderElectionID:               cfg.LeaderElectionName,
        LeaderElectionNamespace:        cfg.LeaderElectionNamespace,
        LeaderElectionReleaseOnCancel:  true,
        LeaderElectionResourceLock:     "leases",
        Metrics:                        server.Options{BindAddress: cfg.MetricsAddr},
        HealthProbeBindAddress:         cfg.HealthProbeAddr,
        Cache: cache.Options{
            ByObject: map[client.Object]cache.ByObject{
                &coordinationv1.Lease{}: {
                    Namespaces: map[string]cache.Config{
                        "kube-node-lease": {},
                    },
                },
            },
        },
    })
    setupIndexers(mgr)
    setupHealthChecks(mgr)
    return &Operator{...}
}

func setupIndexers(mgr ctrl.Manager) {
    mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, "status.providerID", ...)
    mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, "spec.providerClassRef.name", ...)
    mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, "metadata.labels[frp.operator.io/exitpool]", ...)
    mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Tunnel{}, "status.assignedExit", ...)
}

func setupHealthChecks(mgr ctrl.Manager) {
    mgr.AddReadyzCheck("manager", waitForCacheSync(mgr))
    mgr.AddReadyzCheck("crd", crdReadinessCheck(mgr))
    mgr.AddHealthzCheck("healthz", healthz.Ping)
    mgr.AddReadyzCheck("readyz", healthz.Ping)
}
```

### No admission webhooks

Validation is **CEL-on-CRD** + status controller (`exitpool/validation`). Drop:
- Current webhook server bootstrap.
- Current `pkg/certs/` self-provisioning PKI.
- `config/webhook/`.

Retain webhook scaffolding for future conversion webhooks (v1alpha1→v1beta1→v1 migrations).

### Webhook removal migration plan

This is a behavioral change. Existing users on v1alpha1 with webhook-enforced validation lose runtime checks for some constraints (e.g. `ImmutableWhenReady`). CEL covers most, but not stateful checks. Solution:
- Move `ImmutableWhenReady` from webhook to a **status-validation controller** that detects and surfaces violation as `Conditions[ValidationFailed]=True`. Reconciler refuses to act on the violating spec until user reverts.
- Document removed webhooks in CHANGELOG.

---

## 11. Feature gates

Plumbing via `--feature-gates=Name=true,Name2=false`. Initial set:

| Gate | Default | Stage | Description |
|------|---------|-------|-------------|
| `StaticReplicas` | false | alpha | Enables `ExitPool.Spec.Replicas` static-mode |
| `ExitRepair` | false | alpha | Enables `node/health` controller for unhealthy-exit auto-replacement |
| `InterruptionHandling` | false | alpha | Provider-event-driven forceful disruption (e.g. DO droplet event stream) |
| `ConsolidationDryRun` | true | beta | Logs consolidation decisions without acting |
| `MultiPoolBinpacking` | true | beta | Cross-pool binpacking when same provider class |

---

## 12. Settings (env / flags)

```
BATCH_IDLE_DURATION=1s
BATCH_MAX_DURATION=10s
KUBE_CLIENT_QPS=200
KUBE_CLIENT_BURST=300
LEADER_ELECTION_NAMESPACE=frp-operator-system
LEADER_ELECTION_NAME=frp-operator-leader
DISABLE_LEADER_ELECTION=false
LOG_LEVEL=info
METRICS_PORT=8080
HEALTH_PROBE_PORT=8081
ENABLE_PROFILING=false
PREFERENCE_POLICY=Respect          # Respect | Ignore
MIN_VALUES_POLICY=Strict           # Strict | BestEffort
DISABLE_CONTROLLER_WARMUP=false
REGISTRATION_TTL=15m
DRIFT_TTL=15m
DISRUPTION_POLL_PERIOD=10s
```

---

## 13. Metrics

Exposed at `:METRICS_PORT/metrics`:

```
# Domain
frp_operator_exitclaim_total{pool, phase}
frp_operator_exitclaim_launch_duration_seconds{pool, provider}
frp_operator_exitclaim_registration_duration_seconds{pool, provider}
frp_operator_provisioning_pending_tunnels
frp_operator_provisioning_solve_duration_seconds
frp_operator_provisioning_decisions_total{result}      # scheduled, unscheduled, failed
frp_operator_disruption_decisions_total{reason, action}
frp_operator_disruption_budget_remaining{pool, reason}
frp_operator_provider_api_calls_total{provider, op, result}
frp_operator_provider_api_duration_seconds{provider, op}
frp_operator_state_cluster_synced_seconds
frp_operator_tunnel_total{phase}
frp_operator_exitpool_resources{pool, dimension}

# Controller-runtime defaults (auto)
controller_runtime_reconcile_total{controller, result}
controller_runtime_reconcile_time_seconds{controller}
workqueue_depth{name}
workqueue_retries_total{name}
leader_election_master_status{name}
```

---

## 14. Failure modes (Status.Conditions surfaces)

User-visible failure surfaces:

| Resource | Condition | Reason | Fix |
|----------|-----------|--------|-----|
| ExitPool | `Ready=False` | `ProviderClassNotFound` | create ProviderClass referenced by template |
| ExitPool | `Ready=False` | `LimitsExceeded` | raise limits or wait for consolidation |
| ExitPool | `ValidationSucceeded=False` | `InvalidRequirements` | fix spec |
| ExitClaim | `Launched=False` | `ProviderError` | check provider credentials, quota, network |
| ExitClaim | `Registered=False` | `RegistrationTimeout` | ExitClaim is being terminated; check provider VM logs |
| ExitClaim | `Initialized=False` | `PortReservationFailed` | check exit AllowPorts vs internal port collisions |
| ExitClaim | `Drifted=True` | `PoolHashMismatch` | informational; replacement queued |
| Tunnel | `Ready=False` | `NoEligibleExit` | scheduler couldn't fit; check pool requirements vs tunnel ports |
| Tunnel | `Ready=False` | `NoMatchingPool` | no ExitPool template matches tunnel requirements |
| Tunnel | `Ready=False` | `BudgetExceeded` | pool limit hit; raise or wait |
| Tunnel | `Ready=False` | `PortConflict` | requested port already allocated on assigned exit |

Events emitted:
- `Provisioning`, `Provisioned`, `ProvisioningFailed`
- `Drifting`, `Consolidating`, `Expiring`, `Disrupting`
- `ProviderError`, `RegistrationTimeout`

---

## 15. Migration

**Not applicable.** Operator has no production deploys. CRD set is rewritten in place. Old `Tunnel`/`ExitServer`/`SchedulingPolicy` (v1alpha1) replaced by new `Tunnel`/`ExitClaim`/`ExitPool` + per-provider `ProviderClass` (v1alpha1). No migration tool, no conversion webhook for the rewrite itself.

Conversion webhook scaffolding will be plumbed for **future** version bumps (v1alpha1 → v1beta1 → v1) once the API stabilizes.

---

## 16. Test scaffolding

Mirror Karpenter's:

```
pkg/test/
├── environment.go                  # envtest harness
├── exitclaim.go                    # fixture builders
├── exitpool.go
├── tunnel.go
├── providerclass.go
└── expectations/
    ├── expectations.go             # ExpectApplied, ExpectScheduled, etc.
    └── matchers.go

pkg/cloudprovider/fake/
├── cloudprovider.go                # in-memory CloudProvider impl
├── instancetype.go
└── providerclass.go

test/e2e/                           # existing kind-based suite, retargeted
├── e2e_suite_test.go
├── fixtures/
└── *_test.go
```

### Test patterns

- Per-controller `suite_test.go` with envtest + Ginkgo:
  ```go
  func TestProvisioning(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Provisioning") }
  var _ = BeforeSuite(func() { env = test.NewEnvironment(test.WithCRDs(apis.CRDs...)) })
  ```
- Fake CloudProvider used by every unit/integration test. No real docker.
- E2E uses real localdocker via kind, exercising full lifecycle.

---

## 17. Out of scope (deferred)

- **Topology spread constraints** for tunnels (cross-region HA). Add in v1beta1 once core stable.
- **Pricing-aware scheduling** (multi-provider cost optimization).
- **Auto-repair** controller (`ExitRepair` feature gate exists but no implementation initially).
- **Static replicas mode** wiring (CRD field exists behind gate, controller refuses if set).
- **Conversion webhook** (scaffolding only, no real migration logic until v1beta1).
- **Interruption handler** (DO droplet event stream, AWS SQS-equivalent).

---

## 18. Open questions

1. **Naming**: `ProviderClass` per-provider or single `ProviderClass` with embedded config? Single-CRD-with-`Type`-discriminator is simpler but less type-safe. Karpenter chose per-provider; we follow.
2. **State.Cluster scope**: cluster-scoped (all exits visible) or namespaced? Karpenter is cluster-scoped (NodeClaim is cluster-scoped). We make ExitClaim cluster-scoped, Tunnel namespaced. Cross-namespace binding via label selector.
3. **Allocations on ExitClaim.Status**: status subresource means status-only updates. Patches must use `--subresource=status`. Acceptable.
4. **Pool template hash algorithm**: SHA256 of canonical-JSON-serialized `Spec.Template.Spec`? Excluded fields list?
5. **Webhook removal timing**: drop in same PR as refactor or keep parallel? Drop in same PR.
6. **Provider rename**: current `internal/provider/{fake,localdocker,digitalocean}` → `pkg/cloudprovider/{fake,localdocker,digitalocean}`. Public package boundary clarifies the contract.

---

## 19. Acceptance criteria

A v1 cut is done when:

1. New CRDs (`ExitPool`, `ExitClaim`, `Tunnel`, `LocalDockerProviderClass`, `DigitalOceanProviderClass`) installed and validated by CEL.
2. Provisioner singleton processes batches of pending Tunnels and binpacks correctly under burst (verified by stress e2e: 20 Tunnels created concurrently → expected min number of ExitClaims, no duplicates).
3. ExitClaim lifecycle controller drives `Created → Launched → Registered → Initialized → Ready` for all providers, with timeouts surfaced as Conditions.
4. Disruption controller correctly:
   - Reclaims empty exits after `ConsolidateAfter`.
   - Detects pool-template drift and triggers replacement.
   - Honors `Disruption.Budgets` (verified by e2e blocking concurrent disruptions).
5. State.Cluster passes a "synced check" gate before every decision.
6. Webhook server removed; CEL validation replaces all admission checks except `ImmutableWhenReady` (moved to status controller).
7. All existing e2e specs ported to new CRD shapes pass on kind + localdocker.
8. Metrics endpoint exposes the catalog in §13.
9. Feature gates plumbed; `--feature-gates` flag works.

---

## 20. References

- Karpenter source: https://github.com/kubernetes-sigs/karpenter
- Karpenter docs: https://karpenter.sh/docs/
- NodePool concepts: https://karpenter.sh/docs/concepts/nodepools/
- NodeClaim concepts: https://karpenter.sh/docs/concepts/nodeclaims/
- Disruption: https://karpenter.sh/docs/concepts/disruption/
- Scheduling: https://karpenter.sh/docs/concepts/scheduling/
- Settings: https://karpenter.sh/docs/reference/settings/
- Metrics: https://karpenter.sh/docs/reference/metrics/
- controller-runtime FAQ on idempotency: https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md
- cert-manager dedup pattern (annotation breadcrumb reference): `pkg/controller/certificates/requestmanager/requestmanager_controller.go`
