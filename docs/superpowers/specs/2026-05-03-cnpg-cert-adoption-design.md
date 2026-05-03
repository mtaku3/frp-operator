# Adopt CloudNativePG webhook + cert + e2e patterns — design

## Context

The current operator deploys validating webhooks behind a cert-manager
Issuer + Certificate. The kustomize stack relies on cert-manager
`replacements` to inject the CA bundle into the
ValidatingWebhookConfiguration. The e2e harness is split into three
build-tag-gated suites (`e2e`, `e2e_localdocker`, `e2e_webhook`) that
each spin up kind separately, with hand-rolled retry-on-error logic
to paper over webhook startup races.

That stack has accumulated bugs: cert-manager install timing,
"failed calling webhook" flakes during BeforeSuite, the operator running
in two topologies (in-cluster for some specs, host-process for
localdocker), and an `--enable-webhooks=false` escape hatch that hides
real wiring problems.

CloudNativePG (CNPG) solves all four problems differently: the operator
self-provisions its CA + leaf cert at startup, the readiness probe
hits the webhook TLS port directly, the kustomize tree drops
cert-manager entirely, and the e2e suite gates `BeforeSuite` on a
positive dry-run admission probe rather than retrying live applies.
The localdocker provider keeps working because the operator still runs
in-cluster — the docker socket is mounted into the manager Pod via an
e2e-only kustomize overlay with a relaxed PodSecurity admission label.

## Goal

Mirror CNPG's approach end-to-end so the operator deploys without
external cert plumbing, the e2e suite is uniform and stable, and the
"three suites with three retry strategies" mess collapses into a
single Ginkgo suite that lives or dies by a single readiness gate.

## Non-goals

- Multi-tenancy / multiple operator replicas. Single-replica leader
  election stays as today.
- Mutating webhooks. We only have validating webhooks; we don't add
  the mutating-webhook code paths CNPG carries.
- Cert renewal without operator restart. CNPG has a renewal goroutine;
  we adopt it. The window is large (one year leaf, 10 years root) and
  we don't optimise this further.
- Out-of-cluster operator process for any production or test path.
  Always in-cluster.

## Architecture

### Cert provisioning (`pkg/certs/`)

A new package, ported almost verbatim from CNPG's `pkg/certs/` (1500
lines, four files plus tests). Renames only:

| CNPG name | Our name |
|---|---|
| `cnpg-ca-secret` | `frp-operator-ca-secret` |
| `cnpg-webhook-cert` | `frp-operator-webhook-cert` |
| `cnpg-webhook-service` | `frp-operator-webhook-service` |
| `cnpg-validating-webhook-configuration` | `frp-operator-validating-webhook-configuration` |
| `app.kubernetes.io/name=cloudnative-pg` (deployment selector) | `app.kubernetes.io/name=frp-operator` |

Files (matching CNPG layout):

- `tls.go` — pure crypto: `createRootCA(commonName)`, `createServerCertificate(ca, dnsNames)`, expiry helpers. No Kubernetes imports.
- `certs.go` — `PublicKeyInfrastructure` struct, `Setup(ctx, client)`, renewal scheduler, certificate validity windows. Wraps `tls.go`.
- `k8s.go` — Secret CRUD, `caBundle` injection into the
  ValidatingWebhookConfiguration, on-disk volume-mount refresh polling.
  Drop CNPG's mutating-webhook branch (we have none).
- `operator_deployment.go` — `SetAsOwnedByOperatorDeployment(secret, deployment)`
  helper so cert Secrets garbage-collect with the operator Deployment.

Tests carry over: `tls_test.go`, `certs_test.go`, `k8s_test.go`,
`operator_deployment_test.go`, plus a shared `suite_test.go` that
registers Ginkgo against this package.

### Operator startup (`cmd/manager/main.go`)

Before `mgr.Start(ctx)` runs:

1. Build a raw `kubeClient` (controller-runtime client without cache).
2. Resolve the operator namespace via the `OPERATOR_NAMESPACE` env var
   (set by manager.yaml's downward API).
3. Build a `certs.PublicKeyInfrastructure` with the renamed constants
   and call `pkiConfig.Setup(ctx, kubeClient)`.
4. Register `/readyz` on `mgr.GetWebhookServer().WebhookMux()` with a
   handler that returns 200 OK once the webhook server's TLS listener
   is up.
5. Wire `TunnelValidator` and `ExitServerValidator` via
   `ctrl.NewWebhookManagedBy[runtime.Object](mgr, &Tunnel{}).WithValidator(v).Complete()`
   (already done in current PR).

The `--enable-webhooks` flag introduced in the previous PR is removed
— webhooks are always on, and the cert-mount-not-yet-refreshed retry
is what bridges first-boot.

### Manager Deployment (`config/manager/manager.yaml`)

```yaml
spec:
  template:
    spec:
      containers:
      - name: manager
        ports:
        - containerPort: 9443
          name: webhook-server
          protocol: TCP
        env:
        - name: OPERATOR_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        livenessProbe:
          httpGet: { path: /readyz, port: 9443, scheme: HTTPS }
        readinessProbe:
          httpGet: { path: /readyz, port: 9443, scheme: HTTPS }
        startupProbe:
          httpGet: { path: /readyz, port: 9443, scheme: HTTPS }
          failureThreshold: 6
          periodSeconds: 5
        volumeMounts:
        - name: webhook-cert
          mountPath: /run/secrets/frp-operator.io/webhook
          readOnly: true
      volumes:
      - name: webhook-cert
        secret:
          secretName: frp-operator-webhook-cert
          optional: true
```

`optional: true` is the trick that lets the Pod start before the
operator has had a chance to create the Secret.

### Kustomize tree

```
config/
├── crd/                     # unchanged
├── rbac/                    # unchanged + Lease, Secrets, ValidatingWebhookConfiguration verbs added
├── manager/
│   ├── manager.yaml         # rewritten (probes, volumes, ports above)
│   └── kustomization.yaml   # unchanged
├── webhook/                 # unchanged (manifests.yaml, service.yaml, kustomization.yaml, kustomizeconfig.yaml)
├── default/
│   └── kustomization.yaml   # crd + rbac + manager + webhook only; no certmanager; no replacements
└── overlays/
    └── e2e/
        ├── kustomization.yaml           # patches manager Deployment
        ├── manager_dockersock_patch.yaml # hostPath:/var/run/docker.sock + matching volumeMount
        └── namespace_psa_patch.yaml      # set frp-operator-system PSA label to "baseline"
```

The default kustomization stays restricted PSA, no docker socket. The
e2e overlay adds the docker socket mount and relaxes PSA so the
operator Pod can pass admission. CI / dev runs `kubectl apply -k config/overlays/e2e`
instead of `make deploy` for the e2e flow; ordinary deploy uses
`config/default`.

`config/certmanager/` is deleted entirely.
`config/default/manager_webhook_patch.yaml` is folded into `manager.yaml`
and deleted.

### RBAC additions

The operator now writes Secrets in its own namespace and patches the
ValidatingWebhookConfiguration. Add to `config/rbac/role.yaml` (via
`+kubebuilder:rbac` markers) cluster-scoped:

```yaml
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["validatingwebhookconfigurations"]
  verbs: ["get", "list", "watch", "update", "patch"]
```

namespace-scoped (lives in `Role`, not `ClusterRole`):

```yaml
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

(Leader election leases live in the operator namespace.)

### E2e harness rewrite

Throw away every file under `test/e2e/`. Replace with a single
Ginkgo suite, one `TestE2E(*testing.T)` entry point, no build tags:

```
test/
├── e2e/
│   ├── e2e_suite_test.go    # BeforeSuite -> operator.WaitForReady; AfterSuite -> tearDown
│   ├── tunnel_test.go        # Describe: Tunnel CRUD, port reservation, finalizer
│   ├── exitserver_test.go    # Describe: ExitServer finalizer + container/cred cleanup
│   ├── scheduling_test.go    # Describe: binpack, AllowPorts refusal
│   ├── reverse_sync_test.go  # Describe: ServiceWatcher → Service ingress IP
│   ├── webhook_test.go       # Describe: ImmutableWhenReady, AllowPorts grow-only
│   ├── traffic_test.go       # Describe: kind-node → frps → frpc → backend
│   ├── resilience_test.go    # Describe: frps restart, frpc reconnect
│   └── fixtures/             # YAML fixtures keyed per test file
└── utils/
    ├── operator/
    │   ├── operator.go       # WaitForReady (Deployment + cert + dry-run probe)
    │   ├── webhooks.go       # checkWebhookSetup, isWebhookWorking
    │   └── doc.go
    ├── tunnel/               # apply, get, wait helpers
    ├── exitserver/           # ditto
    ├── policy/               # SchedulingPolicy helpers
    └── kubernetes/           # generic apply / wait / delete with retry
```

Single build tag `e2e`. One Makefile target `make test-e2e`. The
existing `test-e2e-localdocker` and `test-e2e-webhook` Makefile
targets are removed.

### `BeforeSuite` readiness gate

`test/utils/operator/operator.go WaitForReady(ctx)` blocks until all
three of:

1. `frp-operator-controller-manager` Deployment has
   `status.readyReplicas >= status.replicas` and the latest
   ReplicaSet's Pod is `condition=Ready`.
2. `checkWebhookSetup`: Secret `frp-operator-webhook-cert` exists,
   `tls.crt` byte-equals every `webhooks[].clientConfig.caBundle` on the
   ValidatingWebhookConfiguration.
3. `isWebhookWorking`: dry-run create of an intentionally-invalid
   Tunnel (e.g. missing `spec.service.name`) returns
   `errors.IsInvalid(err) == true` AND the error string contains a
   substring from our validator (e.g. `"spec.service"`). Anything else
   — `connection refused`, `x509`, plain `Forbidden` — counts as not-ready.

Wrapped in `retry.New(Delay=1s, Attempts=120)`. Until this passes, no
spec runs. Specs themselves never retry on webhook errors; if a real
admission rejection happens we want the spec to fail visibly.

### `AfterSuite` and per-spec cleanup

`AfterSuite` deletes the operator namespace and cluster-scoped CRDs
+ ValidatingWebhookConfiguration via server-side apply with
`--force-conflicts`. Each `Describe` cleans up its own CRs in
`AfterAll` using namespace-scoped delete with `--wait=true`. This is
the pattern used by every CNPG test file we surveyed.

### docker.sock plumbing for in-cluster localdocker

The e2e overlay (`config/overlays/e2e`) is the only deployment path
that mounts `/var/run/docker.sock` into the operator Pod. The PSA
label on `frp-operator-system` is set to `baseline` (not
`restricted`) for the same overlay only. Default deployment stays
locked down. Documented in `config/overlays/e2e/README.md`.

### CI workflow

`.github/workflows/e2e.yml` (renamed from current `e2e.yml` /
`tests.yml` if needed):

```yaml
- uses: helm/kind-action@v1.14.0
  with: { install_only: true }
- run: make test-e2e
```

`make test-e2e` does `kind create cluster`, `make docker-build`,
`kind load`, `kubectl apply -k config/overlays/e2e --server-side --force-conflicts`,
then `go test -tags=e2e ./test/e2e/... -v -ginkgo.v -timeout=20m`.

No `helm install cert-manager`. No `make test-e2e-localdocker`.

### Manifest apply mode

All `kubectl apply` invocations in Makefile and tests use
`--server-side --force-conflicts`. Server-side apply avoids the
256 KB last-applied-configuration annotation limit on big CRDs. CNPG
does this everywhere; we adopt it.

## Data flow

1. Operator Pod starts. Kubelet mounts `webhook-cert` Secret if it
   exists; otherwise the volume is empty (`optional: true`).
2. `main()` runs `pki.Setup(ctx, kubeClient)`. Setup creates / reads
   the CA Secret, signs / reads the leaf cert Secret, patches the
   ValidatingWebhookConfiguration's `caBundle`. If the on-disk mount
   doesn't yet match the in-memory cert (first boot), Setup waits for
   kubelet refresh.
3. `mgr.Start()` starts. Webhook server's TLS listener comes up.
   `/readyz` on the webhook mux returns 200 once the server is
   accepting connections.
4. Kubelet probe succeeds. Pod becomes Ready.
5. Tests run: BeforeSuite waits for Deployment Ready + cert-injection
   match + dry-run admission probe. Each spec then issues plain
   `kubectl apply` / client-go calls.

## Error handling

- **PKI failure**: `pki.Setup` returns an error → `setupLog.Error` →
  process exits non-zero. K8s restarts the Pod and we try again.
- **Cert renewal failure**: renewal goroutine logs error, retries
  next cycle. The serving cert keeps working until expiry.
- **Webhook server fails to start**: kubelet probes fail → Pod stays
  NotReady → traffic doesn't get routed. Eventually liveness kicks
  the Pod.
- **caBundle drift** (someone hand-edits the WebhookConfiguration):
  renewal goroutine re-injects on next cycle.

## Testing

### Unit
- `pkg/certs/*_test.go` — port from CNPG, adapt names. Cover CA gen,
  server-cert gen, expiry math, Secret round-trip, `caBundle`
  injection.
- `internal/webhook/v1alpha1/*_test.go` — already exists, no change.

### Integration
- `internal/controller/...` envtest suite — already exists, no
  change. Webhooks are off in envtest by default.

### E2E
- New scaffold described above. Run via `make test-e2e` against a kind
  cluster with the e2e overlay.

## Migration notes

- The `--enable-webhooks=false` flag is removed. Anything depending on
  it must move to either the e2e overlay or accept that webhooks are
  always on.
- `config/certmanager/` is deleted — anyone using cert-manager
  externally to mint certs for our webhook needs to use the
  `optional: true` Secret name as their target.
- `make test-e2e-localdocker` and `make test-e2e-webhook` Makefile
  targets are removed. CI must update if it referenced them.
- The `e2e_localdocker` and `e2e_webhook` build tags go away.

## Open follow-ups

- Cert renewal observability: consider exposing renewal status as a
  metric. Out of scope for this change.
- Webhook conversion strategies (CRD versioning) — we're still on
  `v1alpha1`; revisit when bumping API.
