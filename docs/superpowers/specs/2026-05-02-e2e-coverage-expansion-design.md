# E2E test coverage expansion — design

## Context

The frp-operator has two existing e2e suites:

- Tag `e2e` (`make test-e2e`): operator runs in-cluster via `make deploy`,
  uses the DigitalOcean provider stub. Existing specs cover Manager
  Pod readiness, Tunnel reconcile-to-Allocating, and ServiceWatcher
  Tunnel creation.
- Tag `e2e_localdocker` (`make test-e2e-localdocker`): operator runs
  out-of-cluster as a host subprocess so it can talk to host Docker.
  `LOCALDOCKER_NETWORK=kind` makes frps reachable from kind Pods.
  Existing specs cover Service→Tunnel→Ready and end-to-end traffic
  flow.

This design adds coverage for: tunnel deletion + port reclaim,
ExitServer destroy via finalizer, multi-tunnel binpack, AllowPorts
refusal, ServiceWatcher reverse-sync, frpc reconnect after frps
restart, Tunnel ImmutableWhenReady webhook, and ExitServer AllowPorts
grow-only webhook.

## Suite split

| Suite | Tag | Reason |
|-------|-----|--------|
| LocalDocker integration | `e2e_localdocker` | Needs real frps; out-of-cluster operator with docker access. |
| Webhook validation | `e2e` (extend existing) | Needs in-cluster operator with serving certs (cert-manager); doesn't need real frps. |

The localdocker suite stays out-of-cluster: in-cluster path is blocked
by needing docker.sock mount + relaxed PodSecurity. The webhook suite
stays in-cluster: serving certs require a Service routable from the
apiserver, which a host process can't easily provide.

A single hybrid suite was rejected: mid-suite teardown/redeploy of the
operator with different topologies is brittle and the setups share
nothing material.

## LocalDocker suite — new specs

All blocks share the existing `BeforeSuite` (build manager binary,
start as host process with `LOCALDOCKER_NETWORK=kind`). Each `Describe`
block uses its own `BeforeAll`/`AfterAll` so state doesn't leak across
concerns. Resources stay in the `default` namespace; names are
prefixed per block to ease debugging.

### Block A — Lifecycle (existing block, add specs)

**A1. Tunnel deletion releases ports.**

- Precondition: Tunnel `lifecycle-tunnel` Ready on exit `E`, port 80 in
  `E.status.allocations`, frpc Deployment `lifecycle-tunnel-frpc`
  exists.
- Action: `kubectl delete tunnel lifecycle-tunnel`.
- Assertion: Eventually the Tunnel CR is gone, port 80 is no longer
  present in `E.status.allocations`, and the frpc Deployment is gone
  (owner-ref cascade or finalizer cleanup).

**A2. ExitServer destroy via finalizer.**

- Precondition: ExitServer `E` exists Ready, no remaining allocations
  (e.g., right after A1 completes — make A2 depend on A1's terminal
  state via shared `BeforeAll` for this Describe block, or set up
  fresh).
- Action: `kubectl delete exitserver E`.
- Assertion: Eventually the CR is gone, the docker container
  `frp-operator-default__E` is gone, and `<E>-credentials` Secret is
  gone (the operator-managed one, not the user-provided
  `local-docker-credentials`).

### Block B — Scheduling

Both specs apply a fresh policy with
`consolidation.reclaimEmpty=false` and named `default` (reclaim
controller hardcodes that name).

**B1. Multi-tunnel binpack onto one exit.**

- Precondition: policy default with `allowPorts: ["80","81","1024-65535"]`,
  `local-docker-credentials` Secret, two backend Deployments
  (`bp-a` on 8080, `bp-b` on 8081).
- Action: apply two LoadBalancer Services `bp-a` (port 80) and `bp-b`
  (port 81) with `loadBalancerClass: frp-operator.io/frp`.
- Assertion: exactly one ExitServer exists; both Tunnels reach Ready;
  both `tunnel.status.assignedExit` equal that exit's name; the exit's
  `status.allocations` has both `"80"` and `"81"`.

**B2. AllowPorts refusal — tunnel stays Allocating.**

- Precondition: policy default with `allowPorts: ["1024-65535"]`
  (excludes port 80).
- Action: apply LoadBalancer Service requesting port 80.
- Assertion: ServiceWatcher creates the Tunnel; Tunnel
  `status.phase=Allocating` for at least 30 seconds; no ExitServer
  with `created-by=tunnel-controller` is ever created. (Reason text in
  `status.message` should mention "AllowPorts" or the port number, but
  exact text is not asserted to keep the test resilient to wording
  changes.)

### Block C — ServiceWatcher reverse-sync

**C1. Service.status.loadBalancer.ingress reflects Tunnel
assignedIP.**

- Precondition: Service Ready (Tunnel Ready), exit on kind net.
- Assertion: `svc.status.loadBalancer.ingress[0].ip` equals the exit's
  `status.publicIP` (kind-net IP). Curl from a kind node to that IP
  succeeds.

This overlaps the existing traffic-flows spec; we keep it because the
explicit assertion that the IP matches `exit.publicIP` (not just "non-empty")
catches reverse-sync regressions where ServiceWatcher could write a
stale or wrong address.

### Block D — Resilience

**D1. frpc reconnects after frps restart.**

- Precondition: Tunnel Ready, curl through ingress returns the
  expected body.
- Action: `docker restart frp-operator-default__<exit>`.
- Assertion: within 90 seconds, curl through ingress returns the body
  again. The Tunnel's `status.phase` may transiently leave Ready; we
  only require it eventually returns to Ready, with a generous timeout
  to absorb frpc's reconnect-backoff and the ExitServer admin probe
  cycle.

This spec is the most likely to be flaky on slow CI; we'll skip it
behind an env var (`E2E_LOCALDOCKER_RESILIENCE=1`) until we see how it
behaves in practice. Default off.

## Webhook suite — new specs

Extends `test/e2e/e2e_test.go` (build tag `e2e`). Requires
cert-manager: `BeforeSuite` already has `setupCertManager` scaffolding
that defaults to skip; we change the default for the webhook block to
install when `E2E_WEBHOOK=1` is set, otherwise the new `Describe`
block calls `Skip`.

Operator deployment is unchanged from existing `e2e` suite (in-cluster
via `make deploy`). The deploy already wires the webhook
configuration; what's missing is just the cert.

### Block E — Webhook validation

**E1. Tunnel.ImmutableWhenReady rejects spec change to a Ready
Tunnel.**

- Precondition: Tunnel applied; status.phase=Ready (we may need to
  patch status directly via the API since real provisioning isn't
  available in this suite — e.g., write `status.phase=Ready` with
  `kubectl patch --subresource=status`).
- Action: `kubectl apply` a Tunnel manifest with a changed
  `spec.service.name`.
- Assertion: command exits non-zero; stderr contains
  `denied the request` and a hint about `ImmutableWhenReady`.

**E2. ExitServer.AllowPorts grow-only.**

- Precondition: ExitServer with `allowPorts: ["1024-65535"]` and
  `status.allocations: {"5000": "ns/t"}`.
- Action: `kubectl patch` to set `allowPorts: ["1024-4999"]` (would
  drop port 5000 from allowed set).
- Assertion: rejected; stderr contains `denied the request` and a
  hint about the allocated port.

(For both, we'll use `kubectl apply --dry-run=server` if patching the
status subresource on a Ready Tunnel proves awkward — server-side
dry-run still runs admission, which is what we're testing.)

## File layout

| File | Build tag | Specs |
|------|-----------|-------|
| `test/e2e/localdocker_e2e_test.go` | `e2e_localdocker` | Existing block + A1, A2, B1, B2, C1, D1 |
| `test/e2e/e2e_test.go` | `e2e` | Existing specs + E1, E2 |
| `test/e2e/localdocker_suite_test.go` | `e2e_localdocker` | Unchanged (suite setup) |
| `test/e2e/e2e_suite_test.go` | `e2e` | Default `CERT_MANAGER_INSTALL_SKIP` flips when `E2E_WEBHOOK=1` |

The localdocker file grows from 2 specs to 8. We'll keep one file but
group via multiple `Describe(..., Ordered)` blocks. If the file grows
past ~600 lines or the blocks start fighting over shared state, split
into per-block files later.

## Out of scope

- Real DigitalOcean provisioning.
- Migration tests (`spec.migrationPolicy` is declared but unused per
  earlier review).
- Failure injection beyond frps restart (network partition, OOMKill,
  etc.).
- Multi-namespace scenarios.

## Risks

- **D1 flakiness.** Mitigated by skipping behind env var until we see
  CI behavior.
- **Webhook cert race.** cert-manager may take longer than expected on
  cold starts; `BeforeSuite` already has retry, but webhook specs may
  need an extra `Eventually` around the first `kubectl apply` while
  the webhook becomes ready.
- **Reclaim controller hardcoded `"default"` policy name** (just
  uncovered in test #d03d178). All test policies in this design use
  name `"default"`. A separate fix to make the reclaim controller
  policy-name-aware should be tracked but isn't part of this design.
