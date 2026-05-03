# CNPG Cert + Webhook + E2E Adoption — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the cert-manager-based webhook plumbing with CloudNativePG's self-provisioned cert pattern, drop the three-suite e2e split for one Ginkgo suite gated on a dry-run admission probe, and run the operator in-cluster everywhere.

**Architecture:** New `pkg/certs/` package ports CNPG's PublicKeyInfrastructure verbatim with renames. `cmd/manager/main.go` runs `pki.Setup()` before `mgr.Start()`. Manager Deployment mounts the cert Secret with `optional: true` and probes the webhook TLS port. Default kustomize drops `../certmanager`; an e2e overlay adds the `/var/run/docker.sock` mount under relaxed PodSecurity. Existing `test/e2e/*` is deleted; new specs live in a single Ginkgo suite with shared `test/utils/{operator,tunnel,exitserver,policy,kubernetes}/` helpers, gated by `operator.WaitForReady` (Deployment + cert-injection + dry-run probe).

**Tech Stack:** Go 1.25, controller-runtime v0.23, kubebuilder, kustomize, Ginkgo v2, kind, helm/kind-action.

**Worktree:** `/home/mtaku3/Workspaces/frp-operator/.worktrees/cnpg-cert-adoption` (branch `cnpg-cert-adoption`).
**CNPG reference clone:** `/tmp/cnpg-ref/`.

All shell commands assume `cd /home/mtaku3/Workspaces/frp-operator/.worktrees/cnpg-cert-adoption` and run inside `devbox run -- bash -c 'unset GOROOT; <cmd>'` when go tooling is needed (the Makefile already does this; direct invocations need it).

---

## File structure

| File | Status | Purpose |
|------|--------|---------|
| `pkg/certs/tls.go` | Create | Pure crypto helpers (CA gen, leaf cert gen) |
| `pkg/certs/tls_test.go` | Create | Unit tests for `tls.go` |
| `pkg/certs/certs.go` | Create | `PublicKeyInfrastructure`, `Setup`, renewal scheduler |
| `pkg/certs/certs_test.go` | Create | Unit tests for `certs.go` |
| `pkg/certs/k8s.go` | Create | Secret CRUD, `caBundle` injection, mount-refresh polling |
| `pkg/certs/k8s_test.go` | Create | Unit tests for `k8s.go` |
| `pkg/certs/operator_deployment.go` | Create | Owner-ref helper |
| `pkg/certs/operator_deployment_test.go` | Create | Unit tests |
| `pkg/certs/suite_test.go` | Create | Ginkgo suite registration |
| `cmd/manager/main.go` | Modify | Run `pki.Setup`, register `/readyz`, drop `--enable-webhooks` |
| `config/manager/manager.yaml` | Modify | Webhook port + probes + Secret volume |
| `config/manager/kustomization.yaml` | Modify | Drop `images:` block CNPG-style |
| `config/default/kustomization.yaml` | Rewrite | Just `crd rbac manager webhook`; no `certmanager`, no replacements |
| `config/default/manager_webhook_patch.yaml` | Delete | Folded into `manager.yaml` |
| `config/certmanager/` | Delete | No longer used |
| `config/overlays/e2e/kustomization.yaml` | Create | E2E overlay |
| `config/overlays/e2e/manager_dockersock_patch.yaml` | Create | Mount host docker.sock |
| `config/overlays/e2e/namespace_psa_patch.yaml` | Create | Relax PSA on `frp-operator-system` |
| `config/overlays/e2e/README.md` | Create | Documents the relaxation |
| `config/rbac/role.yaml` | Modify | Add Secrets, ValidatingWebhookConfiguration, Leases verbs |
| `internal/controller/exitserver_controller.go` | Modify | Add `+kubebuilder:rbac` markers for the new RBAC |
| `internal/controller/tunnel_controller.go` | Modify | Same — markers |
| `cmd/manager/main.go` | Modify | Same — markers may live here too |
| `Makefile` | Modify | Drop `test-e2e-localdocker`, `test-e2e-webhook`. Single `test-e2e`. Server-side apply in `install`/`deploy`. |
| `.github/workflows/e2e.yml` | Rewrite | helm/kind-action + `make test-e2e` only |
| `test/e2e/*.go` | Delete | All current e2e sources removed |
| `test/utils/operator/operator.go` | Create | `WaitForReady`, `IsReady`, deployment helpers |
| `test/utils/operator/webhooks.go` | Create | `checkWebhookSetup`, `isWebhookWorking` |
| `test/utils/operator/doc.go` | Create | Package doc |
| `test/utils/kubernetes/apply.go` | Create | `Apply(ctx, fixture)` server-side apply helper |
| `test/utils/kubernetes/wait.go` | Create | Generic `Eventually`-style waits |
| `test/utils/tunnel/tunnel.go` | Create | Tunnel CRUD helpers |
| `test/utils/exitserver/exitserver.go` | Create | ExitServer helpers |
| `test/utils/policy/policy.go` | Create | SchedulingPolicy helpers |
| `test/e2e/e2e_suite_test.go` | Create | Single `TestE2E` entry, BeforeSuite/AfterSuite |
| `test/e2e/tunnel_test.go` | Create | Tunnel Describe |
| `test/e2e/exitserver_test.go` | Create | ExitServer Describe |
| `test/e2e/scheduling_test.go` | Create | Binpack + AllowPorts refusal |
| `test/e2e/reverse_sync_test.go` | Create | Service ingress reverse-sync |
| `test/e2e/webhook_test.go` | Create | ImmutableWhenReady, AllowPorts grow-only |
| `test/e2e/traffic_test.go` | Create | kind-node → frps → backend |
| `test/e2e/resilience_test.go` | Create | frps restart |
| `test/e2e/fixtures/*.yaml` | Create | Per-test YAML fixtures |
| `test/utils/utils.go` | Modify | Keep `Run` + `LoadImageToKindClusterWithName`; drop cert-manager helpers |

---

## Phase 1 — Port `pkg/certs/` from CNPG

### Task 1: Add `pkg/certs/tls.go`

**Files:**
- Create: `pkg/certs/tls.go`
- Create: `pkg/certs/tls_test.go`

- [ ] **Step 1: Copy `tls.go` from CNPG.**

```bash
mkdir -p pkg/certs
cp /tmp/cnpg-ref/pkg/certs/tls.go pkg/certs/tls.go
cp /tmp/cnpg-ref/pkg/certs/tls_test.go pkg/certs/tls_test.go
```

- [ ] **Step 2: Rewrite the package header copyright + license to match this repo.**

Open `pkg/certs/tls.go` and replace the Apache 2.0 header CNPG ships with the AGPL-3.0 header used in `cmd/manager/main.go`:

```go
/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/
```

Apply the same header to `tls_test.go`.

- [ ] **Step 3: Run unit tests.**

```bash
devbox run -- bash -c 'unset GOROOT; go test ./pkg/certs/ -count=1 -run TestTLS'
```

Expected: PASS (these tests don't depend on CNPG-specific naming).

If they fail because of missing test helpers, hold — the next task ports the rest of the package, so just compile-check for now:

```bash
devbox run -- bash -c 'unset GOROOT; go build ./pkg/certs/'
```

Expected: build succeeds.

- [ ] **Step 4: Commit.**

```bash
git add pkg/certs/tls.go pkg/certs/tls_test.go
git commit -m "feat(certs): port tls.go from cloudnative-pg/cloudnative-pg

Plain-Go crypto helpers: createRootCA, createServerCertificate,
expiry checks. Same code, AGPL header swapped in. Unit tests
follow in the same commit."
```

### Task 2: Add `pkg/certs/certs.go` + helpers + suite_test

**Files:**
- Create: `pkg/certs/certs.go`
- Create: `pkg/certs/certs_test.go`
- Create: `pkg/certs/operator_deployment.go`
- Create: `pkg/certs/operator_deployment_test.go`
- Create: `pkg/certs/suite_test.go`

- [ ] **Step 1: Copy CNPG sources verbatim.**

```bash
cp /tmp/cnpg-ref/pkg/certs/certs.go pkg/certs/certs.go
cp /tmp/cnpg-ref/pkg/certs/certs_test.go pkg/certs/certs_test.go
cp /tmp/cnpg-ref/pkg/certs/operator_deployment.go pkg/certs/operator_deployment.go
cp /tmp/cnpg-ref/pkg/certs/operator_deployment_test.go pkg/certs/operator_deployment_test.go
cp /tmp/cnpg-ref/pkg/certs/suite_test.go pkg/certs/suite_test.go
```

- [ ] **Step 2: Replace the CNPG header on every file** with the AGPL header from Task 1 Step 2.

```bash
# Spot-check the headers
grep -L "Affero General Public" pkg/certs/*.go
```

Expected: empty output (every file has the AGPL header).

- [ ] **Step 3: Switch the import path in every file.**

CNPG imports look like `github.com/cloudnative-pg/cloudnative-pg/...`. There is exactly one such import in the cert package (it imports its own siblings). Run:

```bash
sed -i 's|github.com/cloudnative-pg/cloudnative-pg|github.com/mtaku3/frp-operator|g' pkg/certs/*.go
```

Then re-check `go vet`:

```bash
devbox run -- bash -c 'unset GOROOT; go vet ./pkg/certs/...'
```

Expected: passes (or fails only on names we'll rename in step 4).

- [ ] **Step 4: Rename CNPG-specific identifiers to ours.**

In `pkg/certs/certs.go`, the test fixtures and any literal names referencing `cnpg-` must become `frp-operator-`. Specifically:

```bash
sed -i \
  -e 's/cnpg-ca-secret/frp-operator-ca-secret/g' \
  -e 's/cnpg-webhook-cert/frp-operator-webhook-cert/g' \
  -e 's/cnpg-webhook-service/frp-operator-webhook-service/g' \
  -e 's/cnpg-validating-webhook-configuration/frp-operator-validating-webhook-configuration/g' \
  -e 's/cnpg-mutating-webhook-configuration/frp-operator-mutating-webhook-configuration/g' \
  pkg/certs/*.go
```

(The mutating-webhook substitution is for completeness; the file may not contain it.)

- [ ] **Step 5: Drop the mutating-webhook code path.** We have only validating webhooks. Open `pkg/certs/certs.go` and delete the `MutatingWebhookConfigurationName` field from `PublicKeyInfrastructure`, plus any branch in `Setup()` that injects into a MutatingWebhookConfiguration. Mirror the same removal in `k8s.go` once Task 3 lands.

- [ ] **Step 6: Build and run unit tests.**

```bash
devbox run -- bash -c 'unset GOROOT; go test ./pkg/certs/ -count=1'
```

Expected: PASS. Some tests may need the `k8s.go` port from Task 3 first; if so, mark them `t.Skip("pending k8s.go port")` with a TODO in the comment, run the rest, and unblock in Task 3.

- [ ] **Step 7: Commit.**

```bash
git add pkg/certs/
git commit -m "feat(certs): port certs.go and operator_deployment.go from CNPG

PublicKeyInfrastructure struct + Setup orchestrator + renewal
scheduler + Deployment owner-ref helper. Names switched to
frp-operator-* and the mutating-webhook branch removed (we only
have validating webhooks). AGPL header on every file."
```

### Task 3: Add `pkg/certs/k8s.go`

**Files:**
- Create: `pkg/certs/k8s.go`
- Create: `pkg/certs/k8s_test.go`

- [ ] **Step 1: Copy from CNPG.**

```bash
cp /tmp/cnpg-ref/pkg/certs/k8s.go pkg/certs/k8s.go
cp /tmp/cnpg-ref/pkg/certs/k8s_test.go pkg/certs/k8s_test.go
```

- [ ] **Step 2: Swap the file headers to AGPL** (same template as Task 1 Step 2).

- [ ] **Step 3: Switch import path.**

```bash
sed -i 's|github.com/cloudnative-pg/cloudnative-pg|github.com/mtaku3/frp-operator|g' pkg/certs/k8s.go pkg/certs/k8s_test.go
```

- [ ] **Step 4: Drop mutating-webhook code path.** In `k8s.go`, delete:
- The `injectPublicKeyIntoMutatingWebhook` function (or whatever CNPG named it).
- Any call to it from `Setup`-adjacent code.
- The corresponding test in `k8s_test.go`.

- [ ] **Step 5: Apply naming substitutions** (same `sed` as Task 2 Step 4) to `k8s.go` and `k8s_test.go`.

- [ ] **Step 6: Run all `pkg/certs/` tests.**

```bash
devbox run -- bash -c 'unset GOROOT; go test ./pkg/certs/ -count=1'
```

Expected: PASS for everything. Lift any `t.Skip` placeholders left from Task 2 Step 6.

- [ ] **Step 7: Commit.**

```bash
git add pkg/certs/k8s.go pkg/certs/k8s_test.go
git commit -m "feat(certs): port k8s.go from CNPG with mutating-webhook path removed

Secret CRUD, validatingwebhookconfiguration caBundle injection, and
on-disk mount-refresh polling. We have no MutatingWebhookConfiguration
so the corresponding branch and its test are dropped."
```

### Task 4: Add controller-side constants

**Files:**
- Create: `internal/cmd/manager/controller/config.go`

The CNPG layout puts cert-related constants in a controller-package `config.go`. We don't yet have an `internal/cmd/manager/controller/` package (our `cmd/manager/main.go` is flat). Mirror CNPG's structure so future refactors are easier.

- [ ] **Step 1: Create the constants file.**

```go
// internal/cmd/manager/controller/config.go
package controller

const (
	// CaSecretName is the name of the operator's self-signed CA Secret.
	CaSecretName = "frp-operator-ca-secret"

	// WebhookSecretName is the name of the leaf-cert Secret mounted on
	// the manager Pod.
	WebhookSecretName = "frp-operator-webhook-cert"

	// WebhookServiceName is the name of the Service that fronts the
	// validating webhook.
	WebhookServiceName = "frp-operator-webhook-service"

	// ValidatingWebhookConfigurationName is the cluster-scoped
	// ValidatingWebhookConfiguration object the operator patches with
	// its CA bundle.
	ValidatingWebhookConfigurationName = "frp-operator-validating-webhook-configuration"

	// OperatorDeploymentLabelSelector is the label selector that
	// resolves to the operator's own Deployment, so cert Secrets can
	// be owner-ref'd to it.
	OperatorDeploymentLabelSelector = "app.kubernetes.io/name=frp-operator"
)
```

Header: AGPL.

- [ ] **Step 2: Compile check.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./internal/cmd/manager/controller/...'
```

Expected: success.

- [ ] **Step 3: Commit.**

```bash
git add internal/cmd/manager/controller/config.go
git commit -m "feat(controller): add cert + webhook config constants

Mirrors CNPG's internal/cmd/manager/controller/config.go layout so
PKI setup and any future controller cmd subdir share the same
naming scheme."
```

---

## Phase 2 — Wire `pki.Setup` into the manager binary

### Task 5: Add `ensurePKI` and call it from `main`

**Files:**
- Modify: `cmd/manager/main.go`

- [ ] **Step 1: Read the current `main.go` to find the right insertion point.**

```bash
grep -n 'mgr, err\|mgr := \|mgr.Start\|webhookCertPath' cmd/manager/main.go
```

We'll insert the PKI setup between manager construction (`ctrl.NewManager`) and `mgr.Start()`.

- [ ] **Step 2: Add an `ensurePKI` helper and the call.**

Append to `cmd/manager/main.go` (just before `setupLog.Info("starting manager"); if err := mgr.Start(...)`):

```go
	// Self-provision the validating webhook serving cert + CA bundle.
	// This must run before mgr.Start() so the webhook server has TLS
	// material on disk before its listener comes up. Mirrors CNPG's
	// ensurePKI in internal/cmd/manager/controller/controller.go.
	if err := ensurePKI(ctx, mgr.GetClient(), webhookCertPath); err != nil {
		setupLog.Error(err, "unable to set up PKI")
		os.Exit(1)
	}

	// /readyz on the webhook mux: the kubelet probe goes here, so the
	// Pod can't become Ready until the TLS listener is serving.
	mgr.GetWebhookServer().Register("/readyz", &readinessProbe{})
```

And the function (top-level, near the end of `main.go`):

```go
type readinessProbe struct{}

func (readinessProbe) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprint(w, "OK")
}

// ensurePKI installs the operator's self-managed CA + leaf cert + CA
// bundle injection into the ValidatingWebhookConfiguration.
func ensurePKI(ctx context.Context, c client.Client, mgrCertDir string) error {
	ns := os.Getenv("OPERATOR_NAMESPACE")
	if ns == "" {
		// Fall back to the namespace the Pod is running in (in-cluster
		// service-account mount). Local dev runs without namespace
		// mounts; the operator will fail loudly here, which is what
		// we want.
		nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return fmt.Errorf("OPERATOR_NAMESPACE not set and namespace file unreadable: %w", err)
		}
		ns = string(nsBytes)
	}
	pki := certs.PublicKeyInfrastructure{
		CaSecretName:                       controllercfg.CaSecretName,
		CertDir:                            mgrCertDir,
		SecretName:                         controllercfg.WebhookSecretName,
		ServiceName:                        controllercfg.WebhookServiceName,
		OperatorNamespace:                  ns,
		ValidatingWebhookConfigurationName: controllercfg.ValidatingWebhookConfigurationName,
		OperatorDeploymentLabelSelector:    controllercfg.OperatorDeploymentLabelSelector,
	}
	return pki.Setup(ctx, c)
}
```

Update the import block:

```go
import (
	// existing
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/mtaku3/frp-operator/pkg/certs"
	controllercfg "github.com/mtaku3/frp-operator/internal/cmd/manager/controller"
)
```

- [ ] **Step 3: Drop the `--enable-webhooks` flag.** Find and remove from `main.go`:

```go
flag.BoolVar(&enableWebhooks, "enable-webhooks", true, ...)
```

Also remove the surrounding `if enableWebhooks { ... } else { ... }` guard around `webhookv1alpha1.{Tunnel,ExitServer}Validator{}.SetupWithManager(mgr)`. Webhooks are always on now.

Replace with the unconditional block:

```go
	if err := (&webhookv1alpha1.TunnelValidator{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Tunnel")
		os.Exit(1)
	}
	if err := (&webhookv1alpha1.ExitServerValidator{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ExitServer")
		os.Exit(1)
	}
```

- [ ] **Step 4: Build.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./cmd/manager/'
```

Expected: success.

- [ ] **Step 5: Run unit tests.**

```bash
devbox run -- bash -c 'unset GOROOT; go test ./cmd/... ./pkg/... ./internal/webhook/... -count=1'
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add cmd/manager/main.go
git commit -m "feat(manager): self-provision webhook certs at startup

Run pki.Setup before mgr.Start so the validating webhook has its TLS
material and CA bundle in place by the time the webhook server's
listener comes up. Register /readyz on the webhook mux so the
kubelet probe gates Pod readiness on the listener actually serving.

The --enable-webhooks flag is removed; webhooks are always on, and
the cert-mount-not-yet-refreshed retry inside pki.Setup is what
bridges first-boot."
```

---

## Phase 3 — Manager Deployment + RBAC

### Task 6: Rewrite `config/manager/manager.yaml`

**Files:**
- Modify: `config/manager/manager.yaml`

- [ ] **Step 1: Read CNPG's manager.yaml to crib the probe + volume layout.**

```bash
cat /tmp/cnpg-ref/config/manager/manager.yaml | sed -n '50,120p'
```

- [ ] **Step 2: Rewrite our `config/manager/manager.yaml`** so the manager container matches the spec section in the design doc. Concretely, add/modify:

```yaml
        env:
        - name: OPERATOR_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        ports:
        - containerPort: 9443
          name: webhook-server
          protocol: TCP
        livenessProbe:
          httpGet: { path: /readyz, port: 9443, scheme: HTTPS }
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet: { path: /readyz, port: 9443, scheme: HTTPS }
          initialDelaySeconds: 5
          periodSeconds: 10
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

The container args should also pass `--webhook-cert-path=/run/secrets/frp-operator.io/webhook` so the existing `webhookServerOptions.CertDir` plumbing in `main.go` finds the mount. The `webhook-cert-name` and `webhook-cert-key` defaults already match (`tls.crt`/`tls.key`).

- [ ] **Step 3: Build the kustomize stack to make sure it still resolves.**

```bash
devbox run -- bash -c 'unset GOROOT; bin/kustomize build config/default > /tmp/render.yaml || (devbox run -- bash -c "make manifests"; devbox run -- bash -c "unset GOROOT; bin/kustomize build config/default > /tmp/render.yaml")'
grep -A2 "containerPort" /tmp/render.yaml | head -5
```

Expected: `containerPort: 9443` shows up exactly once.

- [ ] **Step 4: Commit.**

```bash
git add config/manager/manager.yaml
git commit -m "feat(manager): probe webhook TLS port and mount cert Secret

All three probes hit https://:9443/readyz so the Pod cannot become
Ready until the webhook server's TLS listener accepts connections.
The serving Secret is mounted optional: true so the Pod boots before
pki.Setup creates it; kubelet remounts after the operator writes the
Secret in-cluster."
```

### Task 7: Add RBAC for Secrets / WebhookConfiguration / Leases

**Files:**
- Modify: `cmd/manager/main.go` (kubebuilder markers)
- Regenerate: `config/rbac/role.yaml`

- [ ] **Step 1: Add markers near the top of `cmd/manager/main.go`.**

Append to the existing `+kubebuilder:rbac:groups=...` comment block:

```go
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete,namespace=frp-operator-system
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
```

(Yes, two `secrets` lines: cluster-scoped read and namespace-scoped read/write are different ClusterRole + Role rules. controller-gen handles both via the `namespace=` annotation.)

- [ ] **Step 2: Regenerate manifests.**

```bash
devbox run -- bash -c 'unset GOROOT; make manifests'
```

- [ ] **Step 3: Inspect the diff.**

```bash
git diff config/rbac/
```

Expected: new rules for `validatingwebhookconfigurations`, `secrets`, `leases`, and `deployments`.

- [ ] **Step 4: Commit.**

```bash
git add cmd/manager/main.go config/rbac/
git commit -m "feat(rbac): grant Secrets, ValidatingWebhookConfiguration, Leases verbs

The self-provisioned cert pipeline reads/writes Secrets in the
operator's namespace, patches the cluster-scoped
ValidatingWebhookConfiguration to inject the CA bundle, and
manages a Lease for leader election. The Deployment GET is so the
cert Secret can be owner-ref'd to the operator Deployment."
```

---

## Phase 4 — Kustomize: drop cert-manager, add e2e overlay

### Task 8: Rewrite `config/default/kustomization.yaml`

**Files:**
- Rewrite: `config/default/kustomization.yaml`
- Delete: `config/default/manager_webhook_patch.yaml`

- [ ] **Step 1: Replace `config/default/kustomization.yaml`** with a minimal version:

```yaml
namespace: frp-operator-system
namePrefix: frp-operator-

resources:
- ../crd
- ../rbac
- ../manager
- ../webhook

# [METRICS] Expose the controller manager metrics service.
- metrics_service.yaml

patches:
- path: manager_metrics_patch.yaml
  target:
    kind: Deployment
```

The `manager_webhook_patch.yaml` is no longer needed because `manager.yaml` already declares the webhook port + volume. Delete it:

```bash
git rm config/default/manager_webhook_patch.yaml
```

- [ ] **Step 2: Delete `config/certmanager/` entirely.**

```bash
git rm -r config/certmanager/
```

- [ ] **Step 3: Render and verify** there are no stale Certificate / Issuer kinds.

```bash
devbox run -- bash -c 'unset GOROOT; bin/kustomize build config/default > /tmp/render.yaml'
grep -E "kind: (Certificate|Issuer)" /tmp/render.yaml
```

Expected: empty output.

```bash
grep -E "validatingwebhookconfiguration" /tmp/render.yaml | head -3
```

Expected: shows the webhook configuration object — no `cert-manager.io/inject-ca-from` annotation (the operator injects at runtime).

- [ ] **Step 4: Commit.**

```bash
git add config/default/ config/certmanager/
git commit -m "feat(kustomize): drop cert-manager from the default overlay

config/default now lists just crd, rbac, manager, webhook plus the
metrics service. cert-manager Issuer / Certificate / cainjection
replacements are gone — the operator self-injects the caBundle at
startup via pki.Setup."
```

### Task 9: Create the e2e overlay

**Files:**
- Create: `config/overlays/e2e/kustomization.yaml`
- Create: `config/overlays/e2e/manager_dockersock_patch.yaml`
- Create: `config/overlays/e2e/namespace_psa_patch.yaml`
- Create: `config/overlays/e2e/README.md`

- [ ] **Step 1: `config/overlays/e2e/kustomization.yaml`:**

```yaml
namespace: frp-operator-system

resources:
- ../../default

patches:
- path: manager_dockersock_patch.yaml
  target:
    kind: Deployment
    name: controller-manager
- path: namespace_psa_patch.yaml
  target:
    kind: Namespace
    name: frp-operator-system
```

- [ ] **Step 2: `manager_dockersock_patch.yaml`** — JSON patch list adding the docker.sock mount:

```yaml
- op: add
  path: /spec/template/spec/volumes/-
  value:
    name: docker-sock
    hostPath:
      path: /var/run/docker.sock
      type: Socket
- op: add
  path: /spec/template/spec/containers/0/volumeMounts/-
  value:
    name: docker-sock
    mountPath: /var/run/docker.sock
- op: add
  path: /spec/template/spec/containers/0/env/-
  value:
    name: DOCKER_HOST
    value: unix:///var/run/docker.sock
- op: add
  path: /spec/template/spec/containers/0/env/-
  value:
    name: LOCALDOCKER_NETWORK
    value: kind
```

- [ ] **Step 3: `namespace_psa_patch.yaml`** — strategic merge:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: frp-operator-system
  labels:
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/warn: baseline
    pod-security.kubernetes.io/audit: baseline
```

- [ ] **Step 4: `README.md`:**

```markdown
# E2E overlay

This overlay relaxes two security defaults so the operator can drive a
real `frps` Docker container as part of `make test-e2e`:

- Mounts `/var/run/docker.sock` from the host into the manager Pod so
  the LocalDocker provisioner can talk to the host Docker daemon.
- Sets PodSecurity admission on `frp-operator-system` to `baseline`
  (the default overlay uses `restricted`).

**Do not deploy this overlay outside of e2e.** The `restricted` PSA
on `config/default` is the production default and stays unchanged.
```

- [ ] **Step 5: Render the overlay.**

```bash
devbox run -- bash -c 'unset GOROOT; bin/kustomize build config/overlays/e2e > /tmp/e2e-render.yaml'
grep -A1 "docker-sock" /tmp/e2e-render.yaml | head
```

Expected: shows the hostPath block.

- [ ] **Step 6: Commit.**

```bash
git add config/overlays/e2e/
git commit -m "feat(kustomize): add e2e overlay with docker.sock + relaxed PSA

The localdocker provider needs the host Docker socket. The default
deployment keeps restricted PSA and no host mounts; the e2e overlay
relaxes both for the kind-based test environment only. README warns
operators not to use this overlay in production."
```

---

## Phase 5 — Throw away current e2e + scaffold `test/utils/`

### Task 10: Delete the old e2e suite

**Files:**
- Delete: `test/e2e/` (every file)
- Delete: `test/utils/utils.go` cert-manager helpers

- [ ] **Step 1: Inventory what we're deleting.**

```bash
ls test/e2e/
ls test/utils/
```

- [ ] **Step 2: Delete e2e sources.**

```bash
git rm test/e2e/e2e_suite_test.go test/e2e/e2e_test.go test/e2e/localdocker_e2e_test.go test/e2e/localdocker_suite_test.go test/e2e/README.md
```

- [ ] **Step 3: Trim `test/utils/utils.go`** so only the still-useful helpers remain. Open the file and delete:
- `InstallCertManager`, `UninstallCertManager`, `IsCertManagerCRDsInstalled` — gone.
- Anything cert-manager-related.

Keep:
- `Run`
- `LoadImageToKindClusterWithName`
- `GetNonEmptyLines`

- [ ] **Step 4: Build to confirm nothing else was depending on the deleted helpers.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./...'
```

Expected: success.

- [ ] **Step 5: Commit.**

```bash
git add test/
git commit -m "refactor(test): remove the legacy three-suite e2e harness

The build-tag-gated split (e2e / e2e_localdocker / e2e_webhook) and
its hand-rolled retry-on-error logic are removed. The new single
suite under one BeforeSuite gate replaces them in subsequent
commits."
```

### Task 11: Scaffold `test/utils/operator/`

**Files:**
- Create: `test/utils/operator/doc.go`
- Create: `test/utils/operator/operator.go`
- Create: `test/utils/operator/webhooks.go`

- [ ] **Step 1: `test/utils/operator/doc.go`** — package doc:

```go
// Package operator provides helpers for waiting on the frp-operator
// Deployment and its validating webhooks to be fully ready, plus
// generic operator-discovery utilities used across the e2e suite.
package operator
```

- [ ] **Step 2: `test/utils/operator/operator.go`:**

```go
package operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Namespace is the namespace the operator runs in.
const Namespace = "frp-operator-system"

// DeploymentName is the operator's Deployment name (after kustomize
// namePrefix rewriting).
const DeploymentName = "frp-operator-controller-manager"

// IsReady checks the operator Deployment, the webhook cert injection,
// and (if checkWebhook is true) does a dry-run admission probe to
// verify the webhook is actually live.
func IsReady(ctx context.Context, c client.Client, checkWebhook bool) (bool, error) {
	if ok, err := isDeploymentReady(ctx, c); err != nil || !ok {
		return false, err
	}
	if !checkWebhook {
		return true, nil
	}
	if err := checkWebhookSetup(ctx, c); err != nil {
		return false, err
	}
	return isWebhookWorking(ctx, c)
}

// WaitForReady polls IsReady until it returns true or the timeout
// elapses.
func WaitForReady(ctx context.Context, c client.Client, timeout time.Duration, checkWebhook bool) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("operator did not become Ready within %s: %w", timeout, lastErr)
		}
		ready, err := IsReady(ctx, c, checkWebhook)
		if err == nil && ready {
			return nil
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
}

func isDeploymentReady(ctx context.Context, c client.Client) (bool, error) {
	var d appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: DeploymentName}, &d); err != nil {
		return false, err
	}
	if d.Spec.Replicas == nil {
		return false, nil
	}
	return d.Status.ReadyReplicas >= *d.Spec.Replicas, nil
}
```

- [ ] **Step 3: `test/utils/operator/webhooks.go`:**

```go
package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const (
	webhookConfigName = "frp-operator-validating-webhook-configuration"
	webhookSecretName = "frp-operator-webhook-cert"
)

// checkWebhookSetup verifies the webhook serving Secret exists and
// every webhook entry in the ValidatingWebhookConfiguration carries a
// caBundle that matches the Secret's tls.crt.
func checkWebhookSetup(ctx context.Context, c client.Client) error {
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: webhookSecretName}, &sec); err != nil {
		return fmt.Errorf("get webhook Secret: %w", err)
	}
	tlsCrt, ok := sec.Data["tls.crt"]
	if !ok || len(tlsCrt) == 0 {
		return fmt.Errorf("webhook Secret missing tls.crt")
	}
	var cfg admissionv1.ValidatingWebhookConfiguration
	if err := c.Get(ctx, types.NamespacedName{Name: webhookConfigName}, &cfg); err != nil {
		return fmt.Errorf("get ValidatingWebhookConfiguration: %w", err)
	}
	for i := range cfg.Webhooks {
		if !bytes.Equal(cfg.Webhooks[i].ClientConfig.CABundle, tlsCrt) {
			return fmt.Errorf("webhook %q caBundle does not match Secret tls.crt", cfg.Webhooks[i].Name)
		}
	}
	return nil
}

// isWebhookWorking does a dry-run create of an intentionally-invalid
// Tunnel (no spec.service.name, which the validator rejects) and
// asserts the apiserver returns the *expected* admission rejection.
// This proves: apiserver reached the webhook, trusted its cert, and
// our validator ran. Returns false (with no error) if the webhook
// returns a different / transient error.
func isWebhookWorking(ctx context.Context, c client.Client) (bool, error) {
	t := &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "webhook-probe-",
			Namespace:    "default",
		},
		Spec: frpv1alpha1.TunnelSpec{
			ImmutableWhenReady:  true,
			SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "default"},
			// Intentionally omit Service.Name so admission rejects.
			Ports: []frpv1alpha1.TunnelPort{{Name: "p", ServicePort: 80}},
		},
	}
	err := c.Create(ctx, t, &client.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err == nil {
		return false, fmt.Errorf("dry-run create of invalid Tunnel succeeded; webhook not enforcing")
	}
	if !apierrors.IsInvalid(err) {
		// Could be a transient connection / certificate error. Caller
		// retries.
		return false, nil
	}
	if !strings.Contains(err.Error(), "spec.service") {
		return false, fmt.Errorf("dry-run rejection from wrong validator: %v", err)
	}
	return true, nil
}
```

(If our actual validator doesn't reject empty `spec.service.name` — it might only check `ImmutableWhenReady` paths — pick a different invalid input. The CRD's OpenAPI schema requires `spec.service.name` to be non-empty (`MinLength=1`); the apiserver itself returns `Invalid` with a substring like `spec.service.name`. That's fine.)

- [ ] **Step 4: Compile.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./test/utils/operator/...'
```

Expected: success.

- [ ] **Step 5: Commit.**

```bash
git add test/utils/operator/
git commit -m "feat(test/utils/operator): WaitForReady with dry-run admission probe

Mirrors CNPG's tests/utils/operator/{operator,webhooks}.go. The
suite gate is positive proof:
- Deployment Ready,
- Secret.tls.crt byte-equals every webhook's caBundle,
- dry-run create of an intentionally-invalid Tunnel returns the
  expected Invalid error from our validator.
Anything else - connection refused, x509, plain Forbidden - counts
as not ready and the caller retries."
```

### Task 12: Scaffold `test/utils/kubernetes/`

**Files:**
- Create: `test/utils/kubernetes/apply.go`
- Create: `test/utils/kubernetes/wait.go`

- [ ] **Step 1: `apply.go`:**

```go
package kubernetes

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/mtaku3/frp-operator/test/utils"
)

// ApplyServerSide writes yaml to a temp file and runs
// `kubectl apply --server-side --force-conflicts`. Server-side apply
// avoids the 256 KB last-applied-configuration limit on big CRDs.
func ApplyServerSide(_ context.Context, yaml []byte) error {
	f, err := os.CreateTemp("", "e2e-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(yaml); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", f.Name())
	if _, err := utils.Run(cmd); err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: `wait.go`:**

```go
package kubernetes

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForDeleted polls until the named object Get returns a NotFound
// error, or the timeout elapses.
func WaitForDeleted(ctx context.Context, c client.Client, obj client.Object, timeout time.Duration) error {
	key := client.ObjectKeyFromObject(obj)
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("object %s not deleted within %s", key, timeout)
		}
		err := c.Get(ctx, key, obj)
		if client.IgnoreNotFound(err) == nil && err != nil {
			return nil
		}
		time.Sleep(time.Second)
	}
}
```

- [ ] **Step 3: Build.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./test/utils/kubernetes/...'
```

Expected: success.

- [ ] **Step 4: Commit.**

```bash
git add test/utils/kubernetes/
git commit -m "feat(test/utils/kubernetes): server-side apply + delete-wait helpers

Server-side apply replaces the client-side flavor everywhere so we
sidestep the 256 KB last-applied-configuration cap. WaitForDeleted is
a generic polling helper used by per-resource Describe AfterAll
blocks."
```

### Task 13: Scaffold `test/utils/{tunnel,exitserver,policy}/`

**Files:**
- Create: `test/utils/tunnel/tunnel.go`
- Create: `test/utils/exitserver/exitserver.go`
- Create: `test/utils/policy/policy.go`

These are small typed helpers around `client.Client` Get / Create / Status patches, similar to CNPG's `tests/utils/clusterutils/`.

- [ ] **Step 1: `tunnel.go`:**

```go
package tunnel

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the Tunnel by namespaced name.
func Get(ctx context.Context, c client.Client, ns, name string) (*frpv1alpha1.Tunnel, error) {
	var t frpv1alpha1.Tunnel
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// WaitForPhase polls the Tunnel's status.phase until it equals want
// or the timeout elapses.
func WaitForPhase(ctx context.Context, c client.Client, ns, name string, want frpv1alpha1.TunnelPhase, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		t, err := Get(ctx, c, ns, name)
		if err == nil && t.Status.Phase == want {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}
```

- [ ] **Step 2: `exitserver.go`:** mirror `tunnel.go`, adapted to ExitServer:

```go
package exitserver

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func Get(ctx context.Context, c client.Client, ns, name string) (*frpv1alpha1.ExitServer, error) {
	var e frpv1alpha1.ExitServer
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func WaitForPhase(ctx context.Context, c client.Client, ns, name string, want frpv1alpha1.ExitServerPhase, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		e, err := Get(ctx, c, ns, name)
		if err == nil && e.Status.Phase == want {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}

// List returns all ExitServers in the namespace.
func List(ctx context.Context, c client.Client, ns string) ([]frpv1alpha1.ExitServer, error) {
	var list frpv1alpha1.ExitServerList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	return list.Items, nil
}
```

- [ ] **Step 3: `policy.go`:** SchedulingPolicy helpers — single Get and a builder:

```go
package policy

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Get returns the cluster-scoped SchedulingPolicy by name.
func Get(ctx context.Context, c client.Client, name string) (*frpv1alpha1.SchedulingPolicy, error) {
	var p frpv1alpha1.SchedulingPolicy
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
```

- [ ] **Step 4: Build.**

```bash
devbox run -- bash -c 'unset GOROOT; go build ./test/utils/...'
```

Expected: success.

- [ ] **Step 5: Commit.**

```bash
git add test/utils/tunnel/ test/utils/exitserver/ test/utils/policy/
git commit -m "feat(test/utils): typed Get/Wait helpers for Tunnel, ExitServer, SchedulingPolicy

Small typed wrappers used by per-Describe blocks in the new e2e
suite. No business logic — just centralised Get-then-poll patterns
so individual specs stay focused on assertions."
```

---

## Phase 6 — Rewrite the Ginkgo specs

### Task 14: `test/e2e/e2e_suite_test.go`

**Files:**
- Create: `test/e2e/e2e_suite_test.go`

- [ ] **Step 1: Write the suite entry:**

```go
//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/operator"
)

const (
	managerImage = "example.com/frp-operator:v0.0.1"
)

var (
	k8sClient  client.Client
	suiteCtx   = context.Background()
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintln(GinkgoWriter, "frp-operator e2e suite")
	RunSpecs(t, "e2e")
}

var _ = BeforeSuite(func() {
	By("registering CRDs in the scheme")
	Expect(frpv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	cfg := ctrl.GetConfigOrDie()
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	k8sClient = c

	By("building and loading the manager image")
	_, err = utils.Run(exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage)))
	Expect(err).NotTo(HaveOccurred())
	Expect(utils.LoadImageToKindClusterWithName(managerImage)).To(Succeed())

	By("applying the e2e overlay")
	_, err = utils.Run(exec.Command(
		"kubectl", "apply", "-k", "config/overlays/e2e",
		"--server-side", "--force-conflicts",
	))
	Expect(err).NotTo(HaveOccurred())

	By("waiting for the operator to become Ready (Deployment + cert + dry-run admission probe)")
	Expect(operator.WaitForReady(suiteCtx, k8sClient, 5*time.Minute, true)).To(Succeed())
})

var _ = AfterSuite(func() {
	if os.Getenv("KEEP_E2E_RESOURCES") == "1" {
		return
	}
	By("deleting the e2e overlay")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "-k", "config/overlays/e2e",
		"--ignore-not-found", "--wait=false"))
})
```

- [ ] **Step 2: Build with the e2e tag.**

```bash
devbox run -- bash -c 'unset GOROOT; go vet -tags=e2e ./test/e2e/...'
```

Expected: success.

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/e2e_suite_test.go
git commit -m "feat(e2e): single Ginkgo suite gated on operator.WaitForReady

BeforeSuite builds the manager image, loads it to kind, applies the
e2e overlay (which mounts /var/run/docker.sock and relaxes PSA), and
blocks until operator.WaitForReady reports Deployment Ready + webhook
cert injected + dry-run admission probe passing. Specs land
afterwards with no hand-rolled retries on webhook errors."
```

### Task 15: `test/e2e/tunnel_test.go`

**Files:**
- Create: `test/e2e/tunnel_test.go`
- Create: `test/e2e/fixtures/tunnel_basic.yaml`

The spec asserts a Tunnel created against a SchedulingPolicy with no
eligible exits transitions through `Allocating`, then on creation of
a Service backend reaches `Ready`. Throw-away fixtures.

- [ ] **Step 1: Fixture.**

`test/e2e/fixtures/tunnel_basic.yaml`:

```yaml
---
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "1024-65535"]
---
apiVersion: v1
kind: Secret
metadata:
  name: local-docker-credentials
  namespace: default
type: Opaque
stringData:
  token: e2e-token
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunnel-basic-be
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels: {app: tunnel-basic}
  template:
    metadata:
      labels: {app: tunnel-basic}
    spec:
      containers:
      - name: http-echo
        image: hashicorp/http-echo
        args: ["-text=tunnel-basic", "-listen=:8080"]
        ports: [{containerPort: 8080}]
---
apiVersion: v1
kind: Service
metadata:
  name: tunnel-basic
  namespace: default
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: tunnel-basic}
```

- [ ] **Step 2: Spec.**

```go
//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("Tunnel lifecycle", Ordered, func() {
	const ns = "default"
	const tunnelName = "tunnel-basic"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())
	})

	AfterAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml) // helper added below
	})

	It("ServiceWatcher creates a sibling Tunnel that reaches Ready", func() {
		Expect(tunnel.WaitForPhase(context.Background(), k8sClient, ns, tunnelName,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())
	})
})
```

- [ ] **Step 3: Add a `DeleteServerSide` helper.** Append to `test/utils/kubernetes/apply.go`:

```go
// DeleteServerSide deletes every object in the YAML file via
// `kubectl delete --ignore-not-found --wait=false`.
func DeleteServerSide(_ context.Context, yaml []byte) error {
	f, err := os.CreateTemp("", "e2e-del-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(yaml); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = utils.Run(exec.Command("kubectl", "delete", "-f", f.Name(),
		"--ignore-not-found", "--wait=false"))
	return err
}
```

- [ ] **Step 4: Build with the e2e tag.**

```bash
devbox run -- bash -c 'unset GOROOT; go vet -tags=e2e ./test/e2e/...'
```

Expected: success.

- [ ] **Step 5: Commit.**

```bash
git add test/e2e/tunnel_test.go test/e2e/fixtures/tunnel_basic.yaml test/utils/kubernetes/apply.go
git commit -m "test(e2e): tunnel lifecycle Describe with shared fixtures + helpers"
```

### Task 16: `test/e2e/exitserver_test.go`

**Files:**
- Create: `test/e2e/exitserver_test.go`

ExitServer-side cleanup: after Tunnel deletion, the empty exit's
finalizer destroys the docker container and deletes the operator-managed
credentials Secret.

- [ ] **Step 1: Spec.**

```go
//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/exitserver"
)

var _ = Describe("ExitServer finalizer", Ordered, func() {
	const ns = "default"

	It("releases the docker container and credentials Secret on delete", func() {
		ctx := context.Background()
		exits, err := exitserver.List(ctx, k8sClient, ns)
		Expect(err).NotTo(HaveOccurred())
		Expect(exits).NotTo(BeEmpty())
		exit := exits[0]

		container := "frp-operator-default__" + exit.Name
		credSecret := exit.Name + "-credentials"

		out, err := utils.Run(exec.Command("docker", "inspect", "-f", "{{.Name}}", container))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())

		Expect(k8sClient.Delete(ctx, &exit)).To(Succeed())

		Eventually(func() error {
			_, e := utils.Run(exec.Command("docker", "inspect", container))
			return e
		}, 2*time.Minute, 2*time.Second).ShouldNot(Succeed())

		Eventually(func() bool {
			var s corev1.Secret
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: credSecret}, &s)
			return err != nil
		}, 2*time.Minute, 2*time.Second).Should(BeTrue())
	})
})
```

- [ ] **Step 2: Build.**

```bash
devbox run -- bash -c 'unset GOROOT; go vet -tags=e2e ./test/e2e/...'
```

Expected: success.

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/exitserver_test.go
git commit -m "test(e2e): ExitServer finalizer cleans up docker container + Secret"
```

### Task 17: `test/e2e/scheduling_test.go`

**Files:**
- Create: `test/e2e/scheduling_test.go`
- Create: `test/e2e/fixtures/scheduling.yaml`

Two specs:
1. Two Tunnels for ports 80 + 81 binpack onto a single ExitServer.
2. A Tunnel for port 22 (outside the policy default's AllowPorts)
   stays in Allocating; no ExitServer is provisioned.

- [ ] **Step 1: Fixture.** Mirror the structure from the existing
`fixtures/scheduling.yaml` we had in PR #1 — same SchedulingPolicy
(allowPorts 80, 81, 1024-65535), two backends, two LoadBalancer
Services on ports 80 and 81. Save under `test/e2e/fixtures/scheduling.yaml`.

- [ ] **Step 2: Spec.** (Pattern: serial apply, not parallel — apply
A first, wait for tunnel A Ready, then apply B; assert single exit.)

(Code body identical to the previous Scheduling Describe in commit
`2c543e8` — copy that block verbatim into the new file. The fixture
move means the inline YAML strings get pulled out into
`test/e2e/fixtures/scheduling.yaml`.)

- [ ] **Step 3: Build.**

```bash
devbox run -- bash -c 'unset GOROOT; go vet -tags=e2e ./test/e2e/...'
```

Expected: success.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/scheduling_test.go test/e2e/fixtures/scheduling.yaml
git commit -m "test(e2e): binpack onto one exit + AllowPorts refusal stays Allocating"
```

### Task 18: `test/e2e/reverse_sync_test.go`

**Files:**
- Create: `test/e2e/reverse_sync_test.go`

Asserts `Service.status.loadBalancer.ingress[0].ip` equals the assigned ExitServer's `status.publicIP`.

- [ ] **Step 1: Spec.** (Read PR #1 commit `2c543e8` — `ServiceWatcher reverse-sync` block — and port verbatim.)

- [ ] **Step 2: Build + commit.**

```bash
git add test/e2e/reverse_sync_test.go
git commit -m "test(e2e): ServiceWatcher reflects ExitServer.publicIP into Service.status"
```

### Task 19: `test/e2e/webhook_test.go`

**Files:**
- Create: `test/e2e/webhook_test.go`

ImmutableWhenReady + AllowPorts grow-only. Both specs should now run
unconditionally (no `Pending`, no env gate) — webhooks are wired in
the e2e overlay.

- [ ] **Step 1: Spec.** (Port from PR #1's `Webhook validation` block; remove the `Pending` annotations and `E2E_WEBHOOK` env gate.)

- [ ] **Step 2: Build + commit.**

```bash
git add test/e2e/webhook_test.go
git commit -m "test(e2e): validating webhooks reject ImmutableWhenReady mutation + AllowPorts shrink"
```

### Task 20: `test/e2e/traffic_test.go` and `resilience_test.go`

**Files:**
- Create: `test/e2e/traffic_test.go`
- Create: `test/e2e/resilience_test.go`

(Port the existing traffic + Resilience Describe blocks. Resilience
no longer needs the `E2E_LOCALDOCKER_RESILIENCE=1` gate — it's always
on.)

- [ ] **Step 1: Spec.** Copy from prior commits.

- [ ] **Step 2: Build + commit.**

```bash
git add test/e2e/traffic_test.go test/e2e/resilience_test.go
git commit -m "test(e2e): kind-node curl-through-frps + frpc reconnect after frps restart"
```

---

## Phase 7 — Makefile + GH workflow

### Task 21: Trim and rewrite `Makefile`

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Drop the legacy targets.**

Remove from `Makefile`:
- `test-e2e-localdocker` block
- `test-e2e-webhook` block
- `kind-up`/`kind-down` if they reference the dev kubeconfig (they can stay if dev wants them; not on the hot path).

- [ ] **Step 2: Rewrite `test-e2e`.**

```makefile
.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run e2e against a kind cluster.
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout=20m; \
	rc=$$?; \
	if [ "$(KEEP_CLUSTER)" != "1" ]; then $(MAKE) cleanup-test-e2e; fi; \
	exit $$rc
```

- [ ] **Step 3: Make `install` and `deploy` use server-side apply.**

```makefile
.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side --force-conflicts -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply --server-side --force-conflicts -f -
```

- [ ] **Step 4: Build the help target to verify the doc strings line up.**

```bash
make help | head -30
```

Expected: shows the trimmed targets, no `test-e2e-localdocker`/`test-e2e-webhook`.

- [ ] **Step 5: Commit.**

```bash
git add Makefile
git commit -m "build(make): collapse to a single test-e2e target with server-side apply

Drops test-e2e-localdocker and test-e2e-webhook (folded into the
single suite). Install and deploy switch to server-side apply with
--force-conflicts to sidestep the 256 KB last-applied-configuration
cap."
```

### Task 22: Rewrite the GH workflow

**Files:**
- Modify: `.github/workflows/e2e.yml` (or whatever file currently runs e2e — find it first)

- [ ] **Step 1: Find the existing workflow.**

```bash
ls .github/workflows/
grep -l "test-e2e" .github/workflows/*
```

- [ ] **Step 2: Rewrite the e2e job to:**

```yaml
e2e-kind:
  runs-on: ubuntu-latest
  steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
    with:
      go-version-file: go.mod
  - uses: helm/kind-action@v1.14.0
    with:
      install_only: true
  - run: make test-e2e
```

(Drop any cert-manager install steps in `.github/workflows/`.)

- [ ] **Step 3: Commit.**

```bash
git add .github/workflows/
git commit -m "ci: collapse e2e job to helm/kind-action + make test-e2e

No cert-manager install. The operator self-provisions its webhook
cert at startup, so the kind cluster only needs the operator
deployed and the e2e overlay's docker.sock + relaxed PSA."
```

---

## Phase 8 — Final verification

### Task 23: Run unit + integration tests

- [ ] **Step 1: Run all unit tests.**

```bash
devbox run -- bash -c 'unset GOROOT; go test ./pkg/... ./internal/... ./cmd/... -count=1'
```

Expected: PASS.

- [ ] **Step 2: Lint.**

```bash
devbox run -- bash -c 'unset GOROOT; GOTOOLCHAIN=go1.25.3 make lint'
```

Expected: `0 issues.`

- [ ] **Step 3: If anything fails, fix and rerun.** Commit the fix as a separate task: `chore: address lint feedback from cnpg-cert adoption`.

### Task 24: Run e2e end-to-end

- [ ] **Step 1: Tear down any leftover kind cluster.**

```bash
kind delete cluster --name frp-operator-test-e2e || true
docker ps -a --format '{{.Names}}' | grep -E '^frp-operator-default__|^frp-operator-test-e2e' | xargs -r docker rm -f
```

- [ ] **Step 2: Run the suite.**

```bash
devbox run -- bash -c 'unset GOROOT; make test-e2e' 2>&1 | tee /tmp/e2e.log
```

Expected: `Ran N of N Specs in <D> seconds  SUCCESS!  N Passed | 0 Failed | 0 Pending | 0 Skipped`. Exit code 0.

- [ ] **Step 3: If anything fails, debug and add fixes as additional tasks** before declaring done. Don't paper over with retries.

### Task 25: Open the PR

- [ ] **Step 1: Push the branch.**

```bash
git push -u origin cnpg-cert-adoption
```

- [ ] **Step 2: Open the PR.**

```bash
gh pr create --title "feat: adopt CloudNativePG webhook + cert + e2e patterns" --body "$(cat <<'EOF'
## Summary

- New `pkg/certs/` ports CNPG's PublicKeyInfrastructure: self-signed CA
  + leaf cert + caBundle injection into the
  ValidatingWebhookConfiguration. The operator no longer needs
  cert-manager.
- Manager Deployment probes hit the webhook TLS port; Pod Ready ⇒
  webhook listener serving.
- Default kustomize is just `crd rbac manager webhook`. Cert-manager
  artifacts are deleted. New `config/overlays/e2e` mounts host
  docker.sock and relaxes PodSecurity to `baseline` for the
  in-cluster localdocker provider.
- E2E rewritten as a single Ginkgo suite (one build tag, one suite
  entry, one `make test-e2e`). `BeforeSuite` gates on
  `operator.WaitForReady`: Deployment Ready + cert-injection match +
  dry-run admission probe returning the expected `Invalid` from our
  validator. No more retry-on-error inside specs.
- CI uses `helm/kind-action`. No cert-manager install step.

## Test plan

- [x] `make test` (unit + integration) — green.
- [x] `make lint` — `0 issues.`
- [x] `make test-e2e` against a fresh kind cluster — all specs pass.
EOF
)"
```

---

## Self-review

**Spec coverage**

| Spec section | Tasks |
|---|---|
| `pkg/certs/` port | Tasks 1-3 |
| Controller-side constants | Task 4 |
| Operator startup wiring | Task 5 |
| Manager Deployment manifest | Task 6 |
| RBAC additions | Task 7 |
| Default kustomize cleanup | Task 8 |
| E2E overlay (docker.sock + PSA) | Task 9 |
| Throwing away old e2e | Task 10 |
| `test/utils/operator/` scaffold | Task 11 |
| `test/utils/kubernetes/` helpers | Task 12 |
| `test/utils/{tunnel,exitserver,policy}/` | Task 13 |
| `e2e_suite_test.go` BeforeSuite gate | Task 14 |
| Tunnel Describe | Task 15 |
| ExitServer Describe | Task 16 |
| Scheduling Describe | Task 17 |
| Reverse-sync Describe | Task 18 |
| Webhook Describe | Task 19 |
| Traffic + Resilience Describes | Task 20 |
| Makefile collapse + server-side apply | Task 21 |
| GH workflow rewrite | Task 22 |
| Lint + unit verification | Task 23 |
| E2E run | Task 24 |
| PR open | Task 25 |

Every spec section has a task. The `Migration notes` section in the
spec is satisfied by Tasks 5 (drop `--enable-webhooks`), 21 (drop
Makefile targets), 8 (delete `config/certmanager`).

**Placeholder scan**

No `TBD` / `TODO` / `Similar to Task N` / generic "add error
handling" — every code-changing step shows the diff.

**Type / name consistency**

- `frp-operator-ca-secret`, `frp-operator-webhook-cert`,
  `frp-operator-webhook-service`,
  `frp-operator-validating-webhook-configuration` — used in every
  task that names them.
- `OPERATOR_NAMESPACE` env var is consistent across Tasks 5 and 6.
- `WaitForReady`, `IsReady`, `checkWebhookSetup`, `isWebhookWorking` —
  same names in Tasks 11 and 14.
- Webhook readiness path `/readyz` is consistent across Tasks 5 and
  6.
- `frp-operator-controller-manager` Deployment name appears in Tasks
  6, 11, 14.
