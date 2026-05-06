# Helm Chart Design — Karpenter-Split Layout

**Date:** 2026-05-06
**Status:** Approved (brainstorming)
**Supersedes:** PR #12 (`charts/frp-operator/` single-chart draft) — to be closed.

## Goal

Ship two Helm charts (`frp-operator-crd`, `frp-operator`) that match karpenter's split-chart pattern exactly. Provide upgradeable CRD lifecycle, hardened operator deployment, optional metrics/networkpolicy/PDB/HPA, and OCI release plumbing.

## Why split

Karpenter ships two charts because templating CRDs in the operator chart's `templates/` would tie CRD lifecycle to operator-release lifecycle (uninstalling operator removes CRDs and cascade-deletes all CRs). Helm 3's `crds/` directory avoids that — but it is install-once: `helm upgrade` never updates `crds/`. Splitting CRDs into their own chart gives both: full Helm upgrade for schema changes, no entanglement with operator install/uninstall.

The operator chart still ships a `crds/` directory as a fallback for users who skip the CRD chart. Helm 3 only applies `crds/` when CRDs do not already exist, so it does nothing when the CRD chart is installed first (the recommended path).

## Repo layout

```
charts/
  frp-operator-crd/
    Chart.yaml          # name=frp-operator-crd, version=X.Y.Z, appVersion=X.Y.Z
    values.yaml
    templates/
      frp.operator.io_exitpools.yaml
      frp.operator.io_exitclaims.yaml
      frp.operator.io_tunnels.yaml
      frp.operator.io_digitaloceanproviderclasses.yaml
      frp.operator.io_localdockerproviderclasses.yaml

  frp-operator/
    Chart.yaml          # name=frp-operator, version=X.Y.Z, appVersion=X.Y.Z
    values.yaml
    crds/               # FALLBACK — same 5 CRDs, plain YAML (no go-template)
      <same 5 files>
    templates/
      _helpers.tpl
      NOTES.txt
      serviceaccount.yaml
      clusterrole-manager.yaml
      clusterrolebinding-manager.yaml
      role-leader-election.yaml
      rolebinding-leader-election.yaml
      deployment.yaml
      service-metrics.yaml          # gated metrics.service.enabled
      servicemonitor.yaml           # gated metrics.serviceMonitor.enabled
      networkpolicy.yaml            # gated networkPolicy.enabled
      poddisruptionbudget.yaml      # gated podDisruptionBudget.enabled
      hpa.yaml                      # gated autoscaling.enabled
```

CRDs duplicated between charts. Single source of truth: `config/crd/bases/`. Sync via Makefile target.

## Chart metadata

Both charts share `version` and `appVersion`, kept identical to the operator binary version. Image tag defaults to `.Chart.AppVersion` so chart upgrades pin operator binary version automatically.

## Values surface

### `frp-operator-crd/values.yaml` (minimal)

```yaml
additionalAnnotations: {}
additionalLabels: {}
```

Both fields injected into each CRD's `metadata` via the template wrapper.

### `frp-operator/values.yaml` (full)

```yaml
nameOverride: ""
fullnameOverride: ""

replicas: 1

image:
  repository: ghcr.io/mtaku3/frp-operator
  tag: ""                   # "" → defaults to .Chart.AppVersion
  pullPolicy: IfNotPresent
  pullSecrets: []

serviceAccount:
  create: true
  name: ""
  annotations: {}

rbac:
  create: true

leaderElection:
  enabled: true
  leaseNamespace: ""        # "" → release namespace; controls --leader-elect-namespace and the leader-election Role's namespace

logLevel: info

resources:
  requests:
    cpu: 10m
    memory: 64Mi
  limits:
    cpu: 500m
    memory: 128Mi

podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]

nodeSelector: {}
tolerations: []
affinity: {}
topologySpreadConstraints: []
priorityClassName: ""
podAnnotations: {}
podLabels: {}
extraArgs: []
extraEnv: []
extraVolumes: []
extraVolumeMounts: []

metrics:
  service:
    enabled: false
    port: 8443
    type: ClusterIP
    annotations: {}
  serviceMonitor:
    enabled: false
    additionalLabels: {}
    interval: 30s
    scrapeTimeout: 10s
    relabelings: []
    metricRelabelings: []

networkPolicy:
  enabled: false
  prometheusNamespace: ""

podDisruptionBudget:
  enabled: false
  minAvailable: 1
  maxUnavailable: ""

autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 3
  targetCPUUtilizationPercentage: 80
```

## Template behavior

### `_helpers.tpl`

- `frp-operator.name` / `frp-operator.fullname` / `frp-operator.chart`
- `frp-operator.labels` — selector labels + `app.kubernetes.io/version` + `helm.sh/chart` + `app.kubernetes.io/managed-by`
- `frp-operator.selectorLabels`
- `frp-operator.serviceAccountName`

### `deployment.yaml`

Derived from `config/manager/manager.yaml`. Differences from current manifest:

- `image: {{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}` (was `controller:latest`).
- args composed from values: `--leader-elect` if `leaderElection.enabled`, `--health-probe-bind-address=:8081`, `--zap-log-level={{ .Values.logLevel }}`, plus any `extraArgs`.
- env: keep `OPERATOR_NAMESPACE` from fieldRef; append `extraEnv`.
- liveness/readiness probes unchanged.
- securityContext / podSecurityContext / resources / nodeSelector / tolerations / affinity / topologySpreadConstraints / priorityClassName / podAnnotations / podLabels / extraVolumes / extraVolumeMounts wired from values.
- imagePullSecrets from `image.pullSecrets`.
- serviceAccountName from `_helpers.tpl`.

### RBAC

Gated by `rbac.create`:

- `ClusterRole` rendered from current `config/rbac/role.yaml` (post-722e0d8 rewrite for new CRD set).
- `ClusterRoleBinding` references the operator ServiceAccount.
- Leader-election `Role` + `RoleBinding` in `leaderElection.leaseNamespace` (release namespace if empty), gated additionally by `leaderElection.enabled`. Operator deployment passes `--leader-elect-namespace` matching the same value.

### Metrics

`service-metrics.yaml` — port `8443` named `https-metrics`, selector = pod selector labels.

`servicemonitor.yaml` — `monitoring.coreos.com/v1`, scheme `https`, standard bearerTokenFile path. Template asserts `metrics.service.enabled` is true when `serviceMonitor.enabled` is true; renders `fail` otherwise.

### NetworkPolicy

`policyTypes: [Ingress]` only — egress unrestricted so the operator can reach DigitalOcean's API, the local Docker socket, and frps admin endpoints on remote exits. Default-deny ingress. Allow-from `prometheusNamespace` (when set) to port 8443. Mirrors `config/network-policy/allow-metrics-traffic.yaml`.

### PodDisruptionBudget

`policy/v1`, selector = pod selector labels. Template asserts exactly one of `minAvailable` / `maxUnavailable` is set; renders `fail` otherwise.

### HPA

`autoscaling/v2`, scale target = operator Deployment. CPU utilization only.

### NOTES.txt

Lists installed CRDs, prints recommended install command for the CRD chart, points at `helm get manifest`, summarizes common toggles.

## CRD generation pipeline

Source of truth: `make manifests` writes `config/crd/bases/frp.operator.io_*.yaml`. The spurious `_fakeproviderclasses.yaml` artifact is excluded by glob (`frp.operator.io_*` matches only real CRDs).

New Makefile target `helm-crds`:

```make
.PHONY: helm-crds
helm-crds: manifests
	@rm -f charts/frp-operator-crd/templates/frp.operator.io_*.yaml
	@rm -f charts/frp-operator/crds/frp.operator.io_*.yaml
	@for f in config/crd/bases/frp.operator.io_*.yaml; do \
	  cp $$f charts/frp-operator/crds/$$(basename $$f); \
	  scripts/wrap-crd-template.sh $$f charts/frp-operator-crd/templates/$$(basename $$f); \
	done
```

`scripts/wrap-crd-template.sh` injects `additionalAnnotations` / `additionalLabels` blocks into each CRD's `metadata` (matching karpenter-crd's wrapping). Idempotent rewrite. The injected blocks look like:

```yaml
metadata:
  annotations:
    {{- with .Values.additionalAnnotations }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
    controller-gen.kubebuilder.io/version: <preserved>
  labels:
    {{- with .Values.additionalLabels }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
  name: <preserved>
```

### Versioning

Both charts pinned to a single version. `Makefile` target `chart-version VERSION=X.Y.Z` rewrites `version` and `appVersion` in both `Chart.yaml` files via sed.

## CI

### Lint job (`.github/workflows/lint.yml`)

```yaml
- uses: azure/setup-helm@v4
- run: helm lint charts/frp-operator-crd
- run: helm lint charts/frp-operator
- run: helm template charts/frp-operator-crd | kubectl apply --dry-run=client -f -
- run: helm template charts/frp-operator | kubectl apply --dry-run=client -f -
- run: make helm-crds && git diff --exit-code charts/
```

The diff-exit-code step is the drift gate — any PR that changes CRD schema must also refresh both chart copies.

### E2E smoke (`.github/workflows/e2e.yml`)

```yaml
- helm install frp-operator-crd charts/frp-operator-crd -n frp-operator-system --create-namespace
- helm install frp-operator     charts/frp-operator     -n frp-operator-system
- kubectl -n frp-operator-system rollout status deploy/frp-operator --timeout=2m
- kubectl apply -f config/samples/localdocker_*.yaml
- kubectl wait --for=condition=Ready tunnel/<sample-name> --timeout=3m
- kubectl get exitclaim,tunnel -A
```

## Release

Trigger: tag `v*` (already used for image build).

Add steps to `.github/workflows/image.yml` (or new `helm-release.yml`):

```yaml
- run: helm package charts/frp-operator-crd --version ${TAG} --app-version ${TAG}
- run: helm package charts/frp-operator     --version ${TAG} --app-version ${TAG}
- run: helm push frp-operator-crd-${TAG}.tgz oci://ghcr.io/mtaku3/charts
- run: helm push frp-operator-${TAG}.tgz     oci://ghcr.io/mtaku3/charts
```

Single registry — same `ghcr.io/mtaku3` namespace as the operator image.

User-facing install command:

```bash
helm upgrade --install frp-operator-crd oci://ghcr.io/mtaku3/charts/frp-operator-crd \
  --version X.Y.Z --namespace frp-operator-system --create-namespace

helm upgrade --install frp-operator oci://ghcr.io/mtaku3/charts/frp-operator \
  --version X.Y.Z --namespace frp-operator-system
```

## Testing strategy

- `helm lint` per chart (CI lint job).
- `helm template | kubectl apply --dry-run=client` (CI lint job — catches schema and templating errors).
- CRD-sync drift gate (CI lint job).
- `helm install` on kind + smoke Tunnel (CI e2e job).
- No template unit tests. Templating is mostly value substitution; CI render + dry-run apply covers the failure surface.

## Out of scope

- `chart-testing` (`ct lint-and-install`) — adds dependency for two charts; not worth it.
- `helm-docs` auto-generation — values.yaml comments are sufficient.
- Multi-version compat matrix — single chart `appVersion` aligned to operator binary.
- Sample CRs in chart — `config/samples/` stays the canonical location.
- Webhook templates — operator currently has no webhook server; `config/webhook/manifests.yaml` is a stale kubebuilder leftover (references deleted `exitservers` CRD). Not included.
- Operator namespace creation by chart — Helm anti-pattern; users pass `--create-namespace`.

## Migration

- Close PR #12 with a note pointing at this design.
- New work happens on a fresh branch (`helm/split-charts` or similar).
- Existing draft chart at `charts/frp-operator/` (single-chart layout) is replaced wholesale by the two-chart layout described here.

## Decisions log

- **Watch scope**: cluster-scoped (matches current operator). No `WATCH_NAMESPACE` value.
- **Namespace creation**: chart does not create. Users pass `--create-namespace`.
- **Image tag default**: `.Chart.AppVersion` — chart version pins binary version (karpenter convention).
- **Sample CRs**: not in chart.
- **Chart names**: `frp-operator-crd` + `frp-operator`.
- **Optional add-ons (A–E)**: all included, all gated by values, all default-off except RBAC and leader-election binding (default-on).
