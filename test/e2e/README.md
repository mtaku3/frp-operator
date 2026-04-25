# E2E tests

End-to-end tests boot a kind cluster, build the operator image, load it, and run scenarios against a real apiserver+kubelet.

## Prerequisites

- Docker.
- `kind` on PATH.
- `kubectl` on PATH.
- Run from repo root: `make test-e2e` (or `go test -tags e2e ./test/e2e/...`).

## What's covered

- Manager Pod reaches Ready in the kind cluster.
- Tunnel CR creation triggers reconcile to at least `Allocating` phase (no eligible exits, expected behavior).
- Service with `loadBalancerClass: frp-operator.io/frp` triggers ServiceWatcherController to create a sibling Tunnel.

## What's NOT covered (deferred)

- LocalDocker provisioner reachability from inside the kind Pod (requires Docker socket mount; varies by kind config).
- frps↔frpc network traffic testing (covered by `internal/provider/localdocker` integration test).
- DigitalOcean provisioner (requires real DO credentials).
- Webhook serving certs (cert-manager install skipped by default).

## Skipping cert-manager

Default behavior. To enable cert-manager install (required for full webhook testing), unset `CERT_MANAGER_INSTALL_SKIP` or set it to `"false"`.

## Cleanup

The suite tears down with AfterAll: undeploy controller, uninstall CRDs, delete namespace. The kind cluster itself is reused if present (`KIND_CLUSTER` env, default `kind`).
