# FRP Operator — v1 Design

**Status:** Draft — 2026-04-23
**Related:** [ADR 0001: Tunnel backend selection](../../adr/0001-tunnel-backend.md)

## 1. Goal

A Kubernetes operator written in Go that exposes in-cluster Services on public
TCP ports by provisioning cloud VPSes running `frps`, with per-tunnel `frpc`
Pods running in-cluster. Modeled on inlets-operator; differs in that the
backend is FRP (see ADR 0001) and in that CRDs are the source of truth, with
the Service-watcher built as a thin layer on top.

Non-goals in v1: reserved/floating IPs, live rebalancing of tunnels across
exits, external (gRPC) strategy plugins, automatic traffic metering,
auto-upgrade of `frps` on live exits, tiered-SKU selection, warm exit pools,
providers other than DigitalOcean.

## 2. Shape

### 2.1 Two user-facing surfaces

1. **CRDs** (foundation): `Tunnel`, `ExitServer`, `SchedulingPolicy`.
2. **Service-watcher** (convenience): translates
   `Service type=LoadBalancer` with `spec.loadBalancerClass:
   frp-operator.io/frp` into a sibling `Tunnel`, and reflects the assigned
   public IP back into `Service.status.loadBalancer.ingress`. Annotations on
   the Service map onto `Tunnel.spec` fields (see §6.2).

### 2.2 Components

- `frps` runs on each VPS. Its lifecycle (provision, config, destroy) is owned
  by the operator, which mutates live config via `frps`'s admin REST API.
- `frpc` runs **in-cluster, one Deployment per `Tunnel`**. Reads its config
  from a per-tunnel `Secret`. Dials the assigned exit. Failure isolation is
  per-tunnel.
- Operator runs in the cluster as a standard Kubebuilder manager.

### 2.3 Provider

First real provider is DigitalOcean. A `Provisioner` Go interface (§5) is the
abstraction; v1 ships three impls: `DigitalOcean`, `External` (register a
pre-existing `frps` on a VPS the user manages), `LocalDocker` (spin up `frps`
containers on the operator's host — used by e2e and for local dev, not
production).

## 3. CRDs

API group: **`frp.operator.io`**. Initial version: `v1alpha1`. Conversion
webhooks deferred until a `v1beta1` exists.

### 3.1 `ExitServer` (namespace-scoped)

```yaml
apiVersion: frp.operator.io/v1alpha1
kind: ExitServer
metadata:
  name: exit-nyc-1
  namespace: frp-operator-system
spec:
  provider: digitalocean          # digitalocean | external | local-docker
  region: nyc1                    # provider-specific region identifier
  size: s-1vcpu-1gb               # provider-specific SKU
  credentialsRef:
    name: do-token                # Secret in same namespace
    key: token
  ssh:
    port: 22                      # metadata only; operator does not SSH in normal operation
  frps:
    version: v0.65.0              # pinned per operator release
    bindPort: 7000
    adminPort: 7500
  allowPorts:                     # allocatable pool; grow-only via admission webhook
    - "1024-65535"
  reservedPorts: [22, 7000, 7500] # operator refuses to allocate these
  capacity:                       # all fields optional; unset = infinity for allocator math
    maxTunnels: 50
    monthlyTrafficGB: 1000
    bandwidthMbps: 1000
status:
  phase: Ready                    # Pending | Provisioning | Ready | Degraded | Unreachable | Lost | Draining | Deleting
  publicIP: 203.0.113.10
  providerID: do-droplet-123456
  frpsVersion: v0.65.0
  allocations:                    # port → tunnel ref (ns/name)
    "443": my-ns/my-tunnel
    "5432": my-ns/pg-tunnel
  usage:                          # sum of current tunnel reservations
    tunnels: 2
    monthlyTrafficGB: 100
    bandwidthMbps: 500
  conditions:                     # standard Kubernetes conditions
    - type: Ready
    - type: Overcommitted | ExitLost | SpecProviderDrift | FrpsUnreachable
  lastReconcileTime: 2026-04-23T18:00:00Z
```

**Field mutability:**

- **Actively reconciled** (edits push to live `frps`): `frps.bindPort`,
  `frps.adminPort`, `allowPorts` (grow-only), `reservedPorts`, `capacity.*`.
- **Metadata only** (edits recorded; no VPS rebuild): `provider`, `region`,
  `size`, `ssh.port`. Drift from last-known provision state raises
  `SpecProviderDrift` condition but is never self-healed destructively.
- **Validated**: `allowPorts` may not shrink below current `status.allocations`
  (admission webhook). That is the only hard validation rule on `ExitServer`.

### 3.2 `Tunnel` (namespace-scoped)

```yaml
apiVersion: frp.operator.io/v1alpha1
kind: Tunnel
metadata:
  name: my-tunnel
  namespace: my-ns
spec:
  service:                        # Service this tunnel exposes
    name: my-svc
    namespace: my-ns              # must equal Tunnel namespace in v1
  ports:
    - name: http
      servicePort: 80             # port on the Service
      publicPort: 80              # port on the exit; defaults to servicePort
      protocol: TCP               # TCP | UDP
  exitRef:                        # hard pin; optional
    name: exit-nyc-1
  placement:                      # soft preferences; optional; ignored if exitRef set
    providers: [digitalocean]
    regions: [nyc1, sfo3]
    sizeOverride: s-2vcpu-2gb     # used only when provisioning a new exit
  schedulingPolicyRef:
    name: default
  requirements:                   # all optional
    monthlyTrafficGB: 100
    bandwidthMbps: 200
  migrationPolicy: Never          # Never | OnOvercommit | OnExitLost | OnOvercommitOrLost
  allowPortSplit: false           # if true, multi-port tunnel may land across multiple exits
status:
  phase: Ready                    # Pending | Allocating | Provisioning | Connecting | Ready | Disconnected | Failed
  assignedExit: exit-nyc-1
  assignedIP: 203.0.113.10
  assignedPorts: [80]
  conditions:
    - type: Ready
    - type: Overcommitted | Disconnected | NoExitAvailable | BudgetExceeded | ExitUnavailable
```

**Semantics:**

- Service port = public port by default; override per-port via `publicPort`.
- `exitRef` is a hard pin: if the named exit can't host the tunnel (port
  conflict or capacity), the tunnel stays `Pending` with
  `ExitUnavailable`. No fallback to another exit.
- `placement` is a filter/preference: allocator filters exits by these before
  ranking, and provisioner uses `sizeOverride` / region preference when
  creating a new exit.
- `migrationPolicy` governs whether the operator re-runs the allocator and
  moves the tunnel on spec changes (`OnOvercommit`) or on exit loss
  (`OnExitLost`). Default `Never` — tunnels stay put. Migration causes public
  IP change and a reconnect window; documented as disruptive.
- Multi-port tunnels default to atomic placement on one exit (one public IP).
  `allowPortSplit: true` permits spreading across exits when no single exit
  can host all ports.

### 3.3 `SchedulingPolicy` (cluster-scoped)

```yaml
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  allocator: CapacityAware        # BinPack | Spread | CapacityAware | Custom:<name>
  provisioner: OnDemand           # OnDemand | FixedPool | Custom:<name>
  budget:
    maxExits: 5                   # hard cap across the cluster
    maxExitsPerNamespace: 2       # optional per-tenant cap
  vps:
    default:
      provider: digitalocean
      regions: [nyc1, sfo3]
      size: s-1vcpu-1gb
      capacity:
        maxTunnels: 50
        monthlyTrafficGB: 1000
        bandwidthMbps: 1000
  consolidation:
    reclaimEmpty: true            # auto-delete exits with zero tunnels
    drainAfter: 10m
  probes:
    adminInterval: 30s            # frps admin-API health probe
    providerInterval: 5m          # provider-API existence probe
    degradedTimeout: 5m           # Degraded → Unreachable after this
    lostGracePeriod: 5m           # Lost-triggered migration waits this long
```

Per-exit reclaim override: annotation `frp-operator.io/reclaim: "false"` on
`ExitServer` disables consolidation for that exit regardless of policy.

## 4. Allocation and provisioning

### 4.1 Port allocation

- Service port = public port. Two tunnels wanting the same public port cannot
  share an exit.
- Allocator checks, for each candidate exit, whether *every* requested public
  port is free (`∉ status.allocations` and `∉ reservedPorts`).
- Reservation is written to `ExitServer.status.allocations` as part of the
  `Tunnel`'s reconcile, using optimistic concurrency (apiserver
  `resourceVersion`). On conflict, requeue.
- Multi-port allocation is atomic per `Tunnel` unless `allowPortSplit: true`.

### 4.2 Capacity

Reservation-based. The operator never measures live traffic. `monthlyTrafficGB`
and `bandwidthMbps` are user-declared on `Tunnel.spec.requirements`; the
allocator checks that `Σ reservations + new ≤ capacity` for each dimension.
Any dimension unset on `ExitServer.spec.capacity` is treated as infinity;
any dimension unset on `Tunnel.spec.requirements` counts as zero.

### 4.3 Allocator (pick among existing exits)

`func(tunnel, []ExitServer) → (*ExitServer, reason)`. Built-ins:

- **BinPack** — densest eligible exit first (fewest free ports).
- **Spread** — sparsest eligible exit first.
- **CapacityAware** (default) — eligibility by all caps; among eligible, order
  by BinPack. No traffic-headroom knob (deferred — see §9).

Eligibility = ports free ∧ `tunnels < maxTunnels` ∧ `Σ traffic reservations ≤
monthlyTrafficGB` ∧ `Σ bandwidth ≤ bandwidthMbps` ∧ matches
`Tunnel.spec.placement`.

### 4.4 Provisioner (what to create when no exit fits)

`func(tunnel, policy, currentExits) → (*ProvisionDecision)`. Built-ins:

- **OnDemand** (default) — provision one exit matching `policy.vps.default`,
  overridden by `Tunnel.spec.placement.sizeOverride` / region preference.
  Refuses (tunnel `Pending` / `BudgetExceeded`) if `maxExits` cap hit.
- **FixedPool** — never provisions beyond a fixed count; tunnels beyond
  capacity stay `Pending`.

### 4.5 Consolidation

Empty-exit reclamation only in v1. When an `ExitServer` holds zero tunnels
and `reclaimEmpty` is true (both policy and per-exit annotation), transition
to `Draining`, wait `drainAfter`, then destroy the VPS. During `Draining`
the allocator treats the exit as ineligible (§8.2), so it cannot pick up new
tunnels via soft scheduling. However, a `Tunnel` with an explicit `exitRef`
pin to the draining exit **does** abort reclamation and returns the exit to
`Ready`. Changing the `reclaim` annotation to `false` mid-drain also aborts.

Live rebalancing (moving running tunnels to consolidate load) is **v2**. It
requires either stable IPs or an accepted IP-change protocol, and is out of
scope for v1.

## 5. Extension points

All custom strategies are compile-time Go plugins: users vendor the operator
module, implement the interface, and register in their fork's `main.go`.
There is no runtime plugin mechanism in v1.

```go
type Provisioner interface {
    Create(ctx, *ExitServer) (ProviderState, error)
    Destroy(ctx, *ExitServer) error
    Inspect(ctx, *ExitServer) (ProviderState, error) // drives Lost detection
}

type Allocator interface {
    Allocate(ctx, *Tunnel, []ExitServer) (*AllocationDecision, error)
}

type ProvisionStrategy interface {
    Plan(ctx, *Tunnel, *SchedulingPolicy, []ExitServer) (*ProvisionDecision, error)
}
```

Strategies are looked up by name in a registry populated at `main.go` init
time. `SchedulingPolicy.spec.allocator: "Custom:my-allocator"` selects a
registered implementation.

## 6. Controllers

### 6.1 Five controllers

1. **ExitServerController** — owns VPS lifecycle: provisions via the
   configured `Provisioner`, bootstraps via cloud-init, probes health
   (admin-API + provider-API), reconciles `frps.toml` via admin API on spec
   change, handles finalizer and teardown.
2. **TunnelController** — owns proxy lifecycle: runs `Allocator` → picks exit
   (or invokes `ProvisionStrategy`) → reserves ports on
   `ExitServer.status.allocations` (optimistic concurrency) → configures proxy
   via `frps` admin API → creates/reconciles the per-tunnel `frpc` Deployment
   and `Secret` → propagates `frpc` health into `Tunnel.status`.
3. **SchedulingPolicyController** — validates policy references; maintains a
   default policy if none exists.
4. **ServiceWatcherController** — watches Services with
   `spec.loadBalancerClass: frp-operator.io/frp`; creates/updates a sibling
   `Tunnel`; writes the exit's public IP into
   `Service.status.loadBalancer.ingress[]` once the `Tunnel` is `Ready`.
5. **ExitReclaimController** — watches exits with zero tunnels; after
   `drainAfter`, drives `Draining` → `Deleting` → destroy.

### 6.2 Service annotations → Tunnel mapping

| Service annotation | Tunnel.spec field |
|---|---|
| `frp-operator.io/exit: <name>` | `exitRef.name` |
| `frp-operator.io/provider: digitalocean` | appended to `placement.providers` |
| `frp-operator.io/region: nyc1,sfo3` | `placement.regions` |
| `frp-operator.io/size: s-2vcpu-2gb` | `placement.sizeOverride` |
| `frp-operator.io/scheduling-policy: <name>` | `schedulingPolicyRef.name` |
| `frp-operator.io/allow-port-split: "true"` | `allowPortSplit` |
| `frp-operator.io/migration-policy: OnExitLost` | `migrationPolicy` |
| `frp-operator.io/traffic-gb: "100"` | `requirements.monthlyTrafficGB` |
| `frp-operator.io/bandwidth-mbps: "200"` | `requirements.bandwidthMbps` |

## 7. Bootstrap and auth

### 7.1 Cloud-init bootstrap

At droplet create, DO receives a cloud-init user-data script that:

1. Fetches the pinned `frps` release tarball by URL; verifies SHA-256 against
   a checksum baked into the operator binary.
2. Installs the binary to `/usr/local/bin/frps`.
3. Writes `/etc/frp/frps.toml` with `bindPort`, `webServer` admin (bound to
   `0.0.0.0`, behind strong token auth), `auth.token` for `frpc` clients.
4. Installs a systemd unit shipped by the operator; enables and starts it.
5. Configures UFW: allow `bindPort`, `adminPort`, and `allowPorts` range.
   Deny everything else.

The operator does not SSH into the VPS in normal operation. If cloud-init
fails, the exit goes to `Failed` after provisioner timeout and is destroyed;
the TunnelController re-runs `ProvisionStrategy` on the next reconcile.

### 7.2 Auth

- **`frps` admin API**: HTTP with a 32-byte random token in v1. Token
  generated per exit at provision time, stored in the per-exit `Secret`.
  **Known limitation**: plaintext admin traffic over public internet means
  the token is sniffable on any intermediate hop. Acceptable as a v1
  tradeoff (HTTPS terminates into `frps` needing a cert lifecycle the
  operator also has to manage); TLS for the admin API is a v2 item (§9).
- **`frpc` ↔ `frps`**: shared token per exit, same generation and storage
  path. Each `frpc` Pod mounts its exit's `Secret`.
- **Rotation**: delete the `Secret`; the operator regenerates and pushes to
  `frps` via admin API, then restarts the `frpc` Pods on that exit (expected
  momentary disconnect). No auto-rotation in v1.

### 7.3 `frps` versioning

Pinned per operator release. The operator ships one `frps` version, its
checksum, and its cloud-init template. Upgrade path: upgrade the operator →
new exits use new version; existing exits keep their installed version. No
in-place upgrade of live exits in v1. To upgrade an existing exit, the user
creates a replacement exit, migrates tunnels (either by deleting/recreating
them or by using `migrationPolicy`), and deletes the old exit.

## 8. Failure handling

### 8.1 Health model

Two independent probes per exit, intervals from `SchedulingPolicy.probes`:

- **Admin API probe** — `GET /api/serverinfo` on `frps` (default 30s).
- **Provider probe** — provider-specific existence check (default 5m). For
  DO: `GET /droplets/<id>`.

### 8.2 Phases and reactions

| `status.phase` | Admin API | Provider | Meaning | Allocator behavior |
|---|---|---|---|---|
| Ready | ok | running | healthy | eligible |
| Degraded | failing < `degradedTimeout` | running | transient | ineligible for new allocations; existing tunnels keep retrying |
| Unreachable | failing ≥ `degradedTimeout` | running | sustained | ineligible; `Disconnected` condition on child tunnels |
| Lost | n/a | 404 / destroyed | VPS gone | ineligible; `ExitLost` condition |
| Draining | — | — | being reclaimed | ineligible |
| Deleting | — | — | finalizer running | ineligible |

Default reaction to `Degraded`/`Unreachable`/`Lost`: emit Kubernetes events
and set conditions. Do **not** migrate, do **not** self-heal by
re-provisioning. Child tunnels stay placed.

Opt-in reactions via `Tunnel.spec.migrationPolicy`:

- `OnExitLost` — on `Lost` for ≥ `lostGracePeriod`, re-run allocator; move
  tunnel to a different exit (provisioning one if necessary). Public IP
  changes.
- `OnOvercommit` — on a spec change that causes the current exit to
  over-commit, re-run allocator and move.
- `OnOvercommitOrLost` — both.

### 8.3 Deletion

**`ExitServer` deletion** is guarded by a finalizer that blocks until:

1. All child `Tunnel`s are either deleted, re-homed (per their
   `migrationPolicy`), or explicitly force-released.
2. The VPS is destroyed via `Provisioner.Destroy`.
3. Per-exit resources (Secret, firewall rules if applicable) are cleaned up.

Annotation override on `ExitServer`: `frp-operator.io/force-delete: "true"`
skips the tunnel-drain gate. Child tunnels transition to `Disconnected`
immediately — user accepts downtime.

**`Tunnel` deletion**: finalizer removes the proxy from `frps` via admin API,
releases the port on `ExitServer.status.allocations`, deletes the `frpc`
Deployment and `Secret`.

## 9. Explicit v2 deferrals

- **Reserved IPs / floating IPs** — the provider-managed IP outlives the
  droplet, giving transparent recovery from `Lost`. Dropped from v1 for
  simplicity.
- **Live tunnel rebalancing** (migrate running tunnels between exits for
  consolidation). Requires either stable IPs or an accepted disruption
  protocol.
- **Traffic measurement and headroom-based allocation** — operator-side
  metering of real traffic, enforcement above declared budget. Complicated
  by future multi-tunnel-per-Service load-balancing semantics.
- **External (gRPC) strategies** — runtime-pluggable `Allocator` /
  `ProvisionStrategy`. Go-interface (compile-time) is enough for v1.
- **Auto-upgrade of `frps` on live exits.** In-place upgrade story is
  non-trivial; v1 requires replacement.
- **Tiered SKU selection / warm exit pools.**
- **Additional providers** (Hetzner, Linode, AWS, GCP). The `Provisioner`
  interface is designed to accommodate them.
- **TLS for the `frps` admin API.** v1 ships HTTP + token; v2 adds a cert
  lifecycle (probably cloud-init-issued self-signed with operator-side pinning
  or ACME for public-DNS exits).

## 10. Testing

### 10.1 Unit

Pure functions with table-driven tests:

- `Allocator` implementations against synthetic `ExitServer` slices.
- `ProvisionStrategy` decision logic.
- Port-allocation math (including conflict on multi-port atomic reservation).
- Cloud-init template rendering.

### 10.2 Controller

`envtest` (real apiserver + etcd binaries, no kubelet) plus two fakes:

- **FakeProvisioner** — in-memory state machine simulating provision/inspect/
  destroy with configurable delays and failure injection.
- **FakeFrpsAdmin** — HTTP server implementing the `frps` admin API contract;
  simulates config reload, proxy listing, traffic counters.

Covers all reconcile paths: happy-path allocation, port conflict, capacity
conflict, exit loss, finalizer blocking, spec-change reconcile, etc. No
real cluster or cloud.

### 10.3 E2E

`kind` cluster plus the `LocalDocker` Provisioner, which spins up real `frps`
containers on the CI host. Real `frpc` Pods run in kind. Tests the full
wire-level behavior end-to-end. Runs on every PR.

Optional nightly job: same suite against real DO behind a credential.

## 11. Repo layout

```
api/v1alpha1/                 # CRD Go types (kubebuilder-generated scaffolding)
cmd/manager/                  # main.go — registers built-in strategies + provisioners
config/                       # CRDs, RBAC, manager manifests (kubebuilder)
internal/controller/          # ExitServer, Tunnel, SchedulingPolicy, ServiceWatcher, ExitReclaim
internal/scheduler/           # Allocator + ProvisionStrategy built-ins and interface
internal/provider/            # Provisioner impls: digitalocean, external, localdocker
internal/frp/                 # frps admin-API client; frpc config rendering
internal/bootstrap/           # cloud-init templates; frps systemd unit
test/e2e/                     # kind + local-docker e2e harness
docs/                         # this file, ADRs, operator runbook
```

## 12. Out-of-band state and the ADR

The ADR sets port assumptions (`bindPort` 7000, admin 7500, `allowPorts`
operator-configured). This design honors them but notes two nuances:

1. The ADR's "allocates a free port from this range" is **not** how v1 works;
   the allocated public port equals the Service port. `allowPorts` defines
   the *permitted* public ports, not a pool from which the operator picks.
2. The ADR implicitly allowed admin-API to bind to a private interface; DO
   does not provide a private network for single droplets. v1 binds admin to
   public with strong-token auth and a UFW rule (§7.1).

Both are refinements to the ADR; we should amend it after this design is
approved.
