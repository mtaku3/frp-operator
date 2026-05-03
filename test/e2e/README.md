# E2E tests

End-to-end tests boot a kind cluster and run scenarios against a real apiserver. Three opt-in suites, each with its own build tag and `make` target.

## Prerequisites

- Docker.
- `kind` on PATH.
- `kubectl` on PATH.
- Run from repo root.

## Suites

### `make test-e2e` (build tag `e2e`)

Operator runs **in-cluster** via `make deploy`. The DigitalOcean provider is stubbed; no real frps traffic.

What it covers:

- Manager Pod reaches Ready in the kind cluster.
- Tunnel CR creation triggers reconcile to at least `Allocating`.
- Service with `loadBalancerClass: frp-operator.io/frp` triggers ServiceWatcher to create a sibling Tunnel.

### `make test-e2e-localdocker` (build tag `e2e_localdocker`)

Operator runs **out-of-cluster** as a host subprocess so it can talk to the host Docker daemon. `LOCALDOCKER_NETWORK=kind` makes provisioned frps containers reachable from kind Pods.

What it covers:

- ServiceWatcher creates a Tunnel and the operator drives it to Ready against a real frps container.
- Traffic flows: kind node → frps → frpc Pod → backend Deployment.
- Tunnel deletion releases its port allocation on the exit (cascade via Service owner-ref).
- ExitServer deletion runs the finalizer (container + cred Secret cleaned).
- A tunnel whose requested port falls outside the policy's default `AllowPorts` stays Allocating; no exit is provisioned.
- Service.status.loadBalancer.ingress reflects the assigned ExitServer.publicIP.
- frpc reconnects after frps container restart.

Pending:

- Multi-tunnel binpack onto a single ExitServer. Unit tests cover the allocator's correctness; the e2e flow has an informer-cache or scheduler race that needs a focused fix before the spec can be enabled.

### `make test-e2e-webhook` (build tag `e2e`, with `E2E_WEBHOOK=1`)

Operator runs **in-cluster**, cert-manager installed in `BeforeSuite` so the validating admission webhooks have serving certs.

What it covers:

- Tunnel `ImmutableWhenReady` rejects spec changes once the Tunnel is Ready.
- ExitServer `AllowPorts` is grow-only with respect to current allocations.

## What's NOT covered (deferred)

- DigitalOcean provisioner (requires real DO credentials).
- Migration tests (Tunnel `MigrationPolicy` declared but unused).
- Failure injection beyond frps restart (network partition, OOMKill).
- Multi-namespace scenarios.

## Cluster lifecycle

Each `make` target runs `setup-test-e2e` to create a kind cluster (default name `frp-operator-test-e2e`, override via `KIND_CLUSTER`) and `cleanup-test-e2e` after the run. Pass `KEEP_CLUSTER=1` to skip teardown for post-mortem inspection.

The localdocker suite reuses the same kind cluster when present, but its `BeforeSuite` scrubs leftover `frp-operator-default__*` Docker containers from prior killed runs so a new ExitServer Provisioner.Create doesn't hit a name conflict.

## Cert-manager opt-out / opt-in

`CERT_MANAGER_INSTALL_SKIP=true` skips cert-manager. The default flips to `false` automatically when `E2E_WEBHOOK=1`. Set `CERT_MANAGER_INSTALL_SKIP` explicitly to override either way.
