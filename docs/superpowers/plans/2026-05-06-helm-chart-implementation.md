# Helm Chart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `charts/frp-operator-crd/` and `charts/frp-operator/` Helm charts following karpenter's split-chart layout, with CI lint + e2e + tag-driven OCI release to `ghcr.io/mtaku3/charts`.

**Architecture:** Two charts in same repo. CRD chart templates the five `frp.operator.io_*` CRDs for full Helm lifecycle. Operator chart owns Deployment / RBAC / SA / optional Service / ServiceMonitor / NetworkPolicy / PodDisruptionBudget / HPA, plus a `crds/` fallback for users who skip the CRD chart. Single source of truth for CRDs is `config/crd/bases/`; a Makefile target syncs both chart copies.

**Tech Stack:** Helm 3 (Chart v2), kubebuilder-generated CRDs, `azure/setup-helm@v4` in CI, OCI registry on GHCR.

**Spec:** `docs/superpowers/specs/2026-05-06-helm-chart-design.md`.

---

### Task 1: Branch + close PR #12

**Files:**
- None (git operations only)

- [ ] **Step 1: Create implementation branch**

```bash
git checkout main
git pull --ff-only origin main
git checkout -b helm/split-charts
```

- [ ] **Step 2: Close PR #12 with note pointing at design**

```bash
gh pr close 12 --comment "Closing in favor of two-chart karpenter-split layout. See docs/superpowers/specs/2026-05-06-helm-chart-design.md and the new branch helm/split-charts."
gh pr view 12 --json state | grep -q CLOSED
```

Expected: state is CLOSED.

- [ ] **Step 3: Sanity check no existing charts/ dir on main**

```bash
test ! -d charts/
```

Expected: exit 0 (directory absent).

---

### Task 2: CRD template wrap script

**Files:**
- Create: `scripts/wrap-crd-template.sh`
- Create: `scripts/wrap-crd-template_test.sh`
- Create: `scripts/testdata/wrap-crd-input.yaml`
- Create: `scripts/testdata/wrap-crd-expected.yaml`

The script takes an input CRD yaml and writes a Helm-templated copy that injects `additionalAnnotations` and `additionalLabels` blocks into the CRD's `metadata`. Idempotent text rewrite.

- [ ] **Step 1: Write the test fixture (input)**

`scripts/testdata/wrap-crd-input.yaml`:

```yaml
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: exitpools.frp.operator.io
spec:
  group: frp.operator.io
  names:
    kind: ExitPool
    listKind: ExitPoolList
    plural: exitpools
    singular: exitpool
  scope: Cluster
  versions: []
```

- [ ] **Step 2: Write the test fixture (expected)**

`scripts/testdata/wrap-crd-expected.yaml`:

```yaml
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    {{- with .Values.additionalAnnotations }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
    controller-gen.kubebuilder.io/version: v0.20.1
  labels:
    {{- with .Values.additionalLabels }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
  name: exitpools.frp.operator.io
spec:
  group: frp.operator.io
  names:
    kind: ExitPool
    listKind: ExitPoolList
    plural: exitpools
    singular: exitpool
  scope: Cluster
  versions: []
```

- [ ] **Step 3: Write the failing test**

`scripts/wrap-crd-template_test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
out=$(mktemp)
trap 'rm -f "$out"' EXIT
./wrap-crd-template.sh testdata/wrap-crd-input.yaml "$out"
diff -u testdata/wrap-crd-expected.yaml "$out"
echo "PASS"
```

```bash
chmod +x scripts/wrap-crd-template_test.sh
```

- [ ] **Step 4: Run test to verify failure**

```bash
bash scripts/wrap-crd-template_test.sh
```

Expected: error — script not found / missing.

- [ ] **Step 5: Implement wrap script**

`scripts/wrap-crd-template.sh`:

```bash
#!/usr/bin/env bash
# Inject additionalAnnotations/additionalLabels Helm template blocks into a
# controller-gen CRD's metadata. Read input, write output. Idempotent: if
# the input already has the blocks, output equals input.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <input.yaml> <output.yaml>" >&2
  exit 2
fi

in=$1
out=$2

awk '
  BEGIN { in_meta = 0; injected_anno = 0; injected_labels = 0 }
  /^metadata:/ { in_meta = 1; print; next }
  in_meta && /^  annotations:/ {
    print
    print "    {{- with .Values.additionalAnnotations }}"
    print "      {{- toYaml . | nindent 4 }}"
    print "    {{- end }}"
    injected_anno = 1
    next
  }
  in_meta && /^  name:/ && !injected_labels {
    print "  labels:"
    print "    {{- with .Values.additionalLabels }}"
    print "      {{- toYaml . | nindent 4 }}"
    print "    {{- end }}"
    injected_labels = 1
    print
    next
  }
  /^[a-zA-Z]/ && !/^metadata:/ { in_meta = 0 }
  { print }
' "$in" > "$out"
```

```bash
chmod +x scripts/wrap-crd-template.sh
```

- [ ] **Step 6: Run test to verify pass**

```bash
bash scripts/wrap-crd-template_test.sh
```

Expected: `PASS`.

- [ ] **Step 7: Commit**

```bash
git add scripts/wrap-crd-template.sh scripts/wrap-crd-template_test.sh scripts/testdata/
git commit -m "feat(helm): CRD template wrap script with golden test"
```

---

### Task 3: Makefile target `helm-crds`

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add target**

Append to `Makefile` (next to other generation targets):

```make
.PHONY: helm-crds
helm-crds: manifests
	@mkdir -p charts/frp-operator-crd/templates charts/frp-operator/crds
	@rm -f charts/frp-operator-crd/templates/frp.operator.io_*.yaml
	@rm -f charts/frp-operator/crds/frp.operator.io_*.yaml
	@for f in config/crd/bases/frp.operator.io_*.yaml; do \
	  cp $$f charts/frp-operator/crds/$$(basename $$f); \
	  scripts/wrap-crd-template.sh $$f charts/frp-operator-crd/templates/$$(basename $$f); \
	done
```

- [ ] **Step 2: Verify target exists in `make help`**

```bash
make help 2>&1 | grep helm-crds || true
```

Expected: appears if Makefile has help system; otherwise non-fatal. Move on.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat(helm): add helm-crds Makefile target"
```

---

### Task 4: CRD chart skeleton

**Files:**
- Create: `charts/frp-operator-crd/Chart.yaml`
- Create: `charts/frp-operator-crd/.helmignore`
- Create: `charts/frp-operator-crd/values.yaml`
- Create: `charts/frp-operator-crd/templates/` (populated by helm-crds)

- [ ] **Step 1: Write `Chart.yaml`**

```yaml
apiVersion: v2
name: frp-operator-crd
description: Custom Resource Definitions for frp-operator
type: application
version: 0.0.1
appVersion: 0.0.1
keywords:
  - frp
  - tunnel
  - operator
home: https://github.com/mtaku3/frp-operator
sources:
  - https://github.com/mtaku3/frp-operator
```

- [ ] **Step 2: Write `.helmignore`**

```
# Patterns to ignore when building packages.
.DS_Store
.git/
.gitignore
.bzr/
.bzrignore
.hg/
.hgignore
.svn/
*.swp
*.bak
*.tmp
*.orig
*~
.project
.idea/
*.tmproj
.vscode/
```

- [ ] **Step 3: Write `values.yaml`**

```yaml
# Annotations to inject into every CRD's metadata.annotations.
additionalAnnotations: {}

# Labels to inject into every CRD's metadata.labels.
additionalLabels: {}
```

- [ ] **Step 4: Generate CRD templates**

```bash
make helm-crds
ls charts/frp-operator-crd/templates/
```

Expected: 5 files matching `frp.operator.io_*.yaml`.

- [ ] **Step 5: Lint and render**

```bash
helm lint charts/frp-operator-crd
helm template t charts/frp-operator-crd | kubectl apply --dry-run=client -f -
```

Expected: lint reports `0 chart(s) failed`; dry-run apply reports five `customresourcedefinition.apiextensions.k8s.io/<name> created (dry run)` lines.

- [ ] **Step 6: Commit**

```bash
git add charts/frp-operator-crd/
git commit -m "feat(helm): add frp-operator-crd chart"
```

---

### Task 5: Operator chart skeleton

**Files:**
- Create: `charts/frp-operator/Chart.yaml`
- Create: `charts/frp-operator/.helmignore`
- Create: `charts/frp-operator/values.yaml`
- Create: `charts/frp-operator/crds/` (populated by helm-crds)

- [ ] **Step 1: Write `Chart.yaml`**

```yaml
apiVersion: v2
name: frp-operator
description: Operator that manages frps tunnels via the four-CRD karpenter-shaped model.
type: application
version: 0.0.1
appVersion: 0.0.1
keywords:
  - frp
  - tunnel
  - operator
home: https://github.com/mtaku3/frp-operator
sources:
  - https://github.com/mtaku3/frp-operator
```

- [ ] **Step 2: Write `.helmignore`** (same content as Task 4 step 2)

- [ ] **Step 3: Write `values.yaml`**

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
  leaseNamespace: ""        # "" → release namespace

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

- [ ] **Step 4: Populate `crds/` fallback**

```bash
make helm-crds
ls charts/frp-operator/crds/
```

Expected: 5 plain YAML CRDs.

- [ ] **Step 5: Commit**

```bash
git add charts/frp-operator/
git commit -m "feat(helm): add frp-operator chart skeleton + crds fallback"
```

---

### Task 6: `_helpers.tpl`

**Files:**
- Create: `charts/frp-operator/templates/_helpers.tpl`

- [ ] **Step 1: Write helpers**

```yaml
{{/*
Chart name (truncated to 63 chars per k8s).
*/}}
{{- define "frp-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. Honors fullnameOverride, then release name + chart name.
*/}}
{{- define "frp-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name + version label.
*/}}
{{- define "frp-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels.
*/}}
{{- define "frp-operator.labels" -}}
helm.sh/chart: {{ include "frp-operator.chart" . }}
{{ include "frp-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "frp-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "frp-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name.
*/}}
{{- define "frp-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "frp-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Leader election lease namespace.
*/}}
{{- define "frp-operator.leaseNamespace" -}}
{{- default .Release.Namespace .Values.leaderElection.leaseNamespace -}}
{{- end -}}
```

- [ ] **Step 2: Render to verify no syntax errors**

```bash
helm template t charts/frp-operator --set rbac.create=false --debug | head -3
```

Expected: any output (helpers are not rendered alone; this is a syntax check only — failure would print template parse errors).

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/_helpers.tpl
git commit -m "feat(helm): _helpers.tpl"
```

---

### Task 7: ServiceAccount

**Files:**
- Create: `charts/frp-operator/templates/serviceaccount.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.serviceAccount.create -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "frp-operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
  {{- with .Values.serviceAccount.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end -}}
```

- [ ] **Step 2: Render and dry-run apply**

```bash
helm template t charts/frp-operator | kubectl apply --dry-run=client -f -
```

Expected: includes `serviceaccount/t-frp-operator created (dry run)`.

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/serviceaccount.yaml
git commit -m "feat(helm): ServiceAccount template"
```

---

### Task 8: ClusterRole + binding (manager)

**Files:**
- Create: `charts/frp-operator/templates/clusterrole-manager.yaml`
- Create: `charts/frp-operator/templates/clusterrolebinding-manager.yaml`

- [ ] **Step 1: Write ClusterRole**

`charts/frp-operator/templates/clusterrole-manager.yaml`:

```yaml
{{- if .Values.rbac.create -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "frp-operator.fullname" . }}-manager
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "patch", "update", "watch"]
- apiGroups: [""]
  resources: ["services/status"]
  verbs: ["get", "patch", "update"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["frp.operator.io"]
  resources:
    - exitclaims
    - exitpools
    - tunnels
    - localdockerproviderclasses
    - digitaloceanproviderclasses
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["frp.operator.io"]
  resources:
    - exitclaims/finalizers
    - exitpools/finalizers
    - tunnels/finalizers
    - localdockerproviderclasses/finalizers
    - digitaloceanproviderclasses/finalizers
  verbs: ["update"]
- apiGroups: ["frp.operator.io"]
  resources:
    - exitclaims/status
    - exitpools/status
    - tunnels/status
    - localdockerproviderclasses/status
    - digitaloceanproviderclasses/status
  verbs: ["get", "patch", "update"]
{{- end -}}
```

- [ ] **Step 2: Write ClusterRoleBinding**

`charts/frp-operator/templates/clusterrolebinding-manager.yaml`:

```yaml
{{- if .Values.rbac.create -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "frp-operator.fullname" . }}-manager
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "frp-operator.fullname" . }}-manager
subjects:
- kind: ServiceAccount
  name: {{ include "frp-operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
{{- end -}}
```

- [ ] **Step 3: Verify drift vs `config/rbac/role.yaml`**

```bash
diff <(helm template t charts/frp-operator | yq 'select(.kind == "ClusterRole") | .rules') <(yq '.rules' config/rbac/role.yaml)
```

Expected: empty diff (rules identical).

- [ ] **Step 4: Render dry-run apply**

```bash
helm template t charts/frp-operator | kubectl apply --dry-run=client -f -
```

Expected: includes `clusterrole.../...-manager created (dry run)` and `clusterrolebinding.../...-manager created (dry run)`.

- [ ] **Step 5: Commit**

```bash
git add charts/frp-operator/templates/clusterrole-manager.yaml charts/frp-operator/templates/clusterrolebinding-manager.yaml
git commit -m "feat(helm): manager ClusterRole + binding"
```

---

### Task 9: Leader-election Role + binding

**Files:**
- Create: `charts/frp-operator/templates/role-leader-election.yaml`
- Create: `charts/frp-operator/templates/rolebinding-leader-election.yaml`

- [ ] **Step 1: Write Role**

`charts/frp-operator/templates/role-leader-election.yaml`:

```yaml
{{- if and .Values.rbac.create .Values.leaderElection.enabled -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "frp-operator.fullname" . }}-leader-election
  namespace: {{ include "frp-operator.leaseNamespace" . }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
{{- end -}}
```

- [ ] **Step 2: Write RoleBinding**

`charts/frp-operator/templates/rolebinding-leader-election.yaml`:

```yaml
{{- if and .Values.rbac.create .Values.leaderElection.enabled -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "frp-operator.fullname" . }}-leader-election
  namespace: {{ include "frp-operator.leaseNamespace" . }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ include "frp-operator.fullname" . }}-leader-election
subjects:
- kind: ServiceAccount
  name: {{ include "frp-operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
{{- end -}}
```

- [ ] **Step 3: Verify gating**

```bash
helm template t charts/frp-operator --set leaderElection.enabled=false | grep -c "leader-election" || true
```

Expected: `0`.

```bash
helm template t charts/frp-operator | grep -c "leader-election"
```

Expected: non-zero.

- [ ] **Step 4: Commit**

```bash
git add charts/frp-operator/templates/role-leader-election.yaml charts/frp-operator/templates/rolebinding-leader-election.yaml
git commit -m "feat(helm): leader-election Role + binding"
```

---

### Task 10: Deployment

**Files:**
- Create: `charts/frp-operator/templates/deployment.yaml`

- [ ] **Step 1: Write template**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "frp-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels:
      {{- include "frp-operator.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        kubectl.kubernetes.io/default-container: manager
        {{- with .Values.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "frp-operator.selectorLabels" . | nindent 8 }}
        {{- with .Values.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      serviceAccountName: {{ include "frp-operator.serviceAccountName" . }}
      terminationGracePeriodSeconds: 10
      {{- with .Values.image.pullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.priorityClassName }}
      priorityClassName: {{ . }}
      {{- end }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
      - name: manager
        image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
        imagePullPolicy: {{ .Values.image.pullPolicy }}
        command: ["/manager"]
        args:
          {{- if .Values.leaderElection.enabled }}
          - --leader-elect
          - --leader-elect-namespace={{ include "frp-operator.leaseNamespace" . }}
          {{- end }}
          - --health-probe-bind-address=:8081
          - --zap-log-level={{ .Values.logLevel }}
          {{- with .Values.extraArgs }}
          {{- toYaml . | nindent 10 }}
          {{- end }}
        env:
        - name: OPERATOR_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        {{- with .Values.extraEnv }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
        securityContext:
          {{- toYaml .Values.securityContext | nindent 10 }}
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          {{- toYaml .Values.resources | nindent 10 }}
        {{- with .Values.extraVolumeMounts }}
        volumeMounts:
          {{- toYaml . | nindent 10 }}
        {{- end }}
      {{- with .Values.extraVolumes }}
      volumes:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.topologySpreadConstraints }}
      topologySpreadConstraints:
        {{- toYaml . | nindent 8 }}
      {{- end }}
```

- [ ] **Step 2: Render with defaults and dry-run apply**

```bash
helm template t charts/frp-operator | kubectl apply --dry-run=client -f -
```

Expected: includes `deployment.apps/t-frp-operator created (dry run)` with no errors.

- [ ] **Step 3: Verify image tag defaults to chart appVersion**

```bash
helm template t charts/frp-operator | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image'
```

Expected: `ghcr.io/mtaku3/frp-operator:0.0.1`.

- [ ] **Step 4: Verify image tag override works**

```bash
helm template t charts/frp-operator --set image.tag=foo | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image'
```

Expected: `ghcr.io/mtaku3/frp-operator:foo`.

- [ ] **Step 5: Commit**

```bash
git add charts/frp-operator/templates/deployment.yaml
git commit -m "feat(helm): Deployment template"
```

---

### Task 11: Metrics Service

**Files:**
- Create: `charts/frp-operator/templates/service-metrics.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.metrics.service.enabled -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "frp-operator.fullname" . }}-metrics
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
  {{- with .Values.metrics.service.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  type: {{ .Values.metrics.service.type }}
  selector:
    {{- include "frp-operator.selectorLabels" . | nindent 4 }}
  ports:
  - name: https-metrics
    port: {{ .Values.metrics.service.port }}
    targetPort: https-metrics
    protocol: TCP
{{- end -}}
```

- [ ] **Step 2: Verify gating**

```bash
helm template t charts/frp-operator | grep -c "kind: Service$" || true
```

Expected: `0` (Service absent by default).

```bash
helm template t charts/frp-operator --set metrics.service.enabled=true | yq 'select(.kind == "Service") | .metadata.name'
```

Expected: `t-frp-operator-metrics`.

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/service-metrics.yaml
git commit -m "feat(helm): metrics Service template"
```

---

### Task 12: ServiceMonitor

**Files:**
- Create: `charts/frp-operator/templates/servicemonitor.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.metrics.serviceMonitor.enabled -}}
{{- if not .Values.metrics.service.enabled -}}
{{- fail "metrics.serviceMonitor.enabled requires metrics.service.enabled=true" -}}
{{- end -}}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "frp-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
    {{- with .Values.metrics.serviceMonitor.additionalLabels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
spec:
  selector:
    matchLabels:
      {{- include "frp-operator.selectorLabels" . | nindent 6 }}
  namespaceSelector:
    matchNames:
    - {{ .Release.Namespace }}
  endpoints:
  - port: https-metrics
    path: /metrics
    scheme: https
    interval: {{ .Values.metrics.serviceMonitor.interval }}
    scrapeTimeout: {{ .Values.metrics.serviceMonitor.scrapeTimeout }}
    bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
    tlsConfig:
      insecureSkipVerify: true
    {{- with .Values.metrics.serviceMonitor.relabelings }}
    relabelings:
      {{- toYaml . | nindent 4 }}
    {{- end }}
    {{- with .Values.metrics.serviceMonitor.metricRelabelings }}
    metricRelabelings:
      {{- toYaml . | nindent 4 }}
    {{- end }}
{{- end -}}
```

- [ ] **Step 2: Verify the cross-flag check fires**

```bash
helm template t charts/frp-operator --set metrics.serviceMonitor.enabled=true 2>&1 | grep -q "requires metrics.service.enabled"
```

Expected: exit 0 (error message present).

- [ ] **Step 3: Verify success path**

```bash
helm template t charts/frp-operator --set metrics.service.enabled=true --set metrics.serviceMonitor.enabled=true | yq 'select(.kind == "ServiceMonitor") | .metadata.name'
```

Expected: `t-frp-operator`.

- [ ] **Step 4: Commit**

```bash
git add charts/frp-operator/templates/servicemonitor.yaml
git commit -m "feat(helm): ServiceMonitor template"
```

---

### Task 13: NetworkPolicy

**Files:**
- Create: `charts/frp-operator/templates/networkpolicy.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.networkPolicy.enabled -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "frp-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      {{- include "frp-operator.selectorLabels" . | nindent 6 }}
  policyTypes:
  - Ingress
  ingress:
  {{- if .Values.networkPolicy.prometheusNamespace }}
  - from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ .Values.networkPolicy.prometheusNamespace }}
    ports:
    - port: 8443
      protocol: TCP
  {{- end }}
{{- end -}}
```

- [ ] **Step 2: Verify gating + render**

```bash
helm template t charts/frp-operator --set networkPolicy.enabled=true --set networkPolicy.prometheusNamespace=monitoring | yq 'select(.kind == "NetworkPolicy") | .spec.policyTypes'
```

Expected: `- Ingress` only (no Egress).

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/networkpolicy.yaml
git commit -m "feat(helm): NetworkPolicy template (ingress-only)"
```

---

### Task 14: PodDisruptionBudget

**Files:**
- Create: `charts/frp-operator/templates/poddisruptionbudget.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.podDisruptionBudget.enabled -}}
{{- if and .Values.podDisruptionBudget.minAvailable .Values.podDisruptionBudget.maxUnavailable -}}
{{- fail "set exactly one of podDisruptionBudget.minAvailable or podDisruptionBudget.maxUnavailable" -}}
{{- end -}}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "frp-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "frp-operator.selectorLabels" . | nindent 6 }}
  {{- with .Values.podDisruptionBudget.minAvailable }}
  minAvailable: {{ . }}
  {{- end }}
  {{- with .Values.podDisruptionBudget.maxUnavailable }}
  maxUnavailable: {{ . }}
  {{- end }}
{{- end -}}
```

- [ ] **Step 2: Verify mutual-exclusion check fires**

```bash
helm template t charts/frp-operator --set podDisruptionBudget.enabled=true --set podDisruptionBudget.minAvailable=1 --set podDisruptionBudget.maxUnavailable=1 2>&1 | grep -q "set exactly one"
```

Expected: exit 0.

- [ ] **Step 3: Verify success path**

```bash
helm template t charts/frp-operator --set podDisruptionBudget.enabled=true | yq 'select(.kind == "PodDisruptionBudget") | .spec.minAvailable'
```

Expected: `1`.

- [ ] **Step 4: Commit**

```bash
git add charts/frp-operator/templates/poddisruptionbudget.yaml
git commit -m "feat(helm): PodDisruptionBudget template"
```

---

### Task 15: HPA

**Files:**
- Create: `charts/frp-operator/templates/hpa.yaml`

- [ ] **Step 1: Write template**

```yaml
{{- if .Values.autoscaling.enabled -}}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "frp-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "frp-operator.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "frp-operator.fullname" . }}
  minReplicas: {{ .Values.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.maxReplicas }}
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: {{ .Values.autoscaling.targetCPUUtilizationPercentage }}
{{- end -}}
```

- [ ] **Step 2: Verify**

```bash
helm template t charts/frp-operator --set autoscaling.enabled=true | yq 'select(.kind == "HorizontalPodAutoscaler") | .spec.maxReplicas'
```

Expected: `3`.

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/hpa.yaml
git commit -m "feat(helm): HPA template"
```

---

### Task 16: NOTES.txt

**Files:**
- Create: `charts/frp-operator/templates/NOTES.txt`

- [ ] **Step 1: Write notes**

```
{{ .Chart.Name }} {{ .Chart.AppVersion }} installed in namespace "{{ .Release.Namespace }}" as release "{{ .Release.Name }}".

Verify the operator is running:

  kubectl -n {{ .Release.Namespace }} rollout status deploy/{{ include "frp-operator.fullname" . }}

CRDs:
  - exitpools.frp.operator.io
  - exitclaims.frp.operator.io
  - tunnels.frp.operator.io
  - localdockerproviderclasses.frp.operator.io
  - digitaloceanproviderclasses.frp.operator.io

If you skipped the frp-operator-crd chart, the CRDs above were installed
from this chart's crds/ directory and will NOT be updated by future
`helm upgrade`. To get upgradeable CRDs, install frp-operator-crd:

  helm upgrade --install frp-operator-crd \
    oci://ghcr.io/mtaku3/charts/frp-operator-crd \
    --version {{ .Chart.AppVersion }} \
    --namespace {{ .Release.Namespace }}

Common toggles:
  --set metrics.service.enabled=true
  --set metrics.serviceMonitor.enabled=true   # requires Prometheus Operator
  --set networkPolicy.enabled=true --set networkPolicy.prometheusNamespace=monitoring
  --set podDisruptionBudget.enabled=true
  --set autoscaling.enabled=true

See `helm get manifest {{ .Release.Name }} -n {{ .Release.Namespace }}` for the full installed object set.
```

- [ ] **Step 2: Render to verify substitution**

```bash
helm install --dry-run --debug t charts/frp-operator -n test 2>&1 | grep -q "installed in namespace"
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add charts/frp-operator/templates/NOTES.txt
git commit -m "feat(helm): NOTES.txt"
```

---

### Task 17: CI lint job

**Files:**
- Modify: `.github/workflows/lint.yml`

- [ ] **Step 1: Add helm-lint job**

Append to `.github/workflows/lint.yml` (sibling to existing `lint` job):

```yaml
  helm-lint:
    name: Helm
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: azure/setup-helm@v4
        with:
          version: v3.16.2
      - name: helm lint
        run: |
          helm lint charts/frp-operator-crd
          helm lint charts/frp-operator
      - name: helm template + dry-run apply
        run: |
          helm template tcrd charts/frp-operator-crd | kubectl apply --dry-run=client -f -
          helm template top  charts/frp-operator     | kubectl apply --dry-run=client -f -
      - name: CRD sync drift gate
        run: |
          make helm-crds
          git diff --exit-code charts/
```

- [ ] **Step 2: Local syntax check on workflow file**

```bash
yq '.' .github/workflows/lint.yml > /dev/null
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/lint.yml
git commit -m "ci(helm): lint + dry-run + CRD drift gate"
```

---

### Task 18: CI e2e helm install path

**Files:**
- Modify: `.github/workflows/e2e.yml`

- [ ] **Step 1: Add helm-install steps to e2e job**

Read the current `.github/workflows/e2e.yml` first; insert these steps after the `kind` cluster is created and the operator image is loaded:

```yaml
      - uses: azure/setup-helm@v4
        with:
          version: v3.16.2
      - name: helm install (CRD chart)
        run: |
          helm upgrade --install frp-operator-crd charts/frp-operator-crd \
            --namespace frp-operator-system --create-namespace
      - name: helm install (operator chart)
        run: |
          helm upgrade --install frp-operator charts/frp-operator \
            --namespace frp-operator-system \
            --set image.repository=$IMAGE_REPO \
            --set image.tag=$IMAGE_TAG
      - name: Wait for operator
        run: |
          kubectl -n frp-operator-system rollout status deploy/frp-operator --timeout=2m
      - name: Smoke — install LocalDocker sample, expect Tunnel Ready
        run: |
          kubectl apply -f config/samples/localdocker_providerclass.yaml
          kubectl apply -f config/samples/localdocker_exitpool.yaml
          kubectl apply -f config/samples/localdocker_tunnel.yaml
          kubectl wait --for=condition=Ready -A --all tunnel --timeout=5m
          kubectl get exitclaim,tunnel -A
```

(Replace `$IMAGE_REPO` / `$IMAGE_TAG` env names if the existing e2e workflow uses different ones — read the file first and align.)

- [ ] **Step 2: Verify yaml validity**

```bash
yq '.' .github/workflows/e2e.yml > /dev/null
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -m "ci(helm): e2e install via charts on kind"
```

---

### Task 19: CI release on tag

**Files:**
- Modify: `.github/workflows/image.yml`

- [ ] **Step 1: Read existing workflow**

```bash
cat .github/workflows/image.yml
```

Locate the tag-trigger job that builds the image. The new helm-publish steps go in a sibling job that runs on the same trigger.

- [ ] **Step 2: Add helm-publish job**

Append (sibling of existing image-build job):

```yaml
  helm-publish:
    name: Publish Helm charts
    if: startsWith(github.ref, 'refs/tags/v')
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
        with:
          version: v3.16.2
      - name: Resolve tag (strip leading v)
        id: vsn
        run: echo "value=${GITHUB_REF_NAME#v}" >> "$GITHUB_OUTPUT"
      - name: Login to GHCR
        run: echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io -u "${{ github.actor }}" --password-stdin
      - name: Package + push CRD chart
        run: |
          helm package charts/frp-operator-crd --version "${{ steps.vsn.outputs.value }}" --app-version "${{ steps.vsn.outputs.value }}"
          helm push frp-operator-crd-${{ steps.vsn.outputs.value }}.tgz oci://ghcr.io/mtaku3/charts
      - name: Package + push operator chart
        run: |
          helm package charts/frp-operator --version "${{ steps.vsn.outputs.value }}" --app-version "${{ steps.vsn.outputs.value }}"
          helm push frp-operator-${{ steps.vsn.outputs.value }}.tgz oci://ghcr.io/mtaku3/charts
```

- [ ] **Step 3: Verify yaml validity**

```bash
yq '.' .github/workflows/image.yml > /dev/null
```

Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/image.yml
git commit -m "ci(helm): publish charts to ghcr.io/mtaku3/charts on tag"
```

---

### Task 20: README install docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Read current README install section**

```bash
grep -n -A2 "## Install\|## Getting Started\|## Quickstart\|helm install" README.md
```

- [ ] **Step 2: Add or update install section**

Insert near the top (after the project description, before any "Development" section). Replace any existing install snippet:

```markdown
## Install

The operator ships as two Helm charts in the karpenter split-chart layout:

- `frp-operator-crd` — the five `frp.operator.io` CRDs, with full Helm lifecycle.
- `frp-operator` — the operator Deployment, RBAC, and optional metrics / NetworkPolicy / PDB / HPA.

```bash
helm upgrade --install frp-operator-crd \
  oci://ghcr.io/mtaku3/charts/frp-operator-crd \
  --version <X.Y.Z> \
  --namespace frp-operator-system --create-namespace

helm upgrade --install frp-operator \
  oci://ghcr.io/mtaku3/charts/frp-operator \
  --version <X.Y.Z> \
  --namespace frp-operator-system
```

The operator chart also ships a `crds/` fallback for users who skip the CRD chart.
Helm 3 only applies fallback CRDs on first install — schema upgrades require the CRD chart.

Common toggles:

| Value | Default | Purpose |
|---|---|---|
| `metrics.service.enabled` | `false` | Expose `:8443/metrics` via a ClusterIP Service |
| `metrics.serviceMonitor.enabled` | `false` | Prometheus Operator scrape (requires `metrics.service.enabled`) |
| `networkPolicy.enabled` | `false` | Default-deny ingress + allow-from `prometheusNamespace` |
| `podDisruptionBudget.enabled` | `false` | Survive node drains |
| `autoscaling.enabled` | `false` | HPA on CPU utilization |
| `leaderElection.enabled` | `true` | Run with `--leader-elect` |

See `charts/frp-operator/values.yaml` for the full surface.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(helm): install instructions"
```

---

### Task 21: Push branch and open PR

**Files:**
- None

- [ ] **Step 1: Push**

```bash
git push -u origin helm/split-charts
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "feat(helm): split-chart install (frp-operator-crd + frp-operator)" --body "$(cat <<'EOF'
## Summary
- Two Helm charts under `charts/`: `frp-operator-crd` (templated CRDs) and `frp-operator` (operator + `crds/` fallback). Karpenter split-chart layout.
- All optional add-ons gated behind values: metrics Service, ServiceMonitor, NetworkPolicy, PodDisruptionBudget, HPA. Default-off (RBAC and leader-election binding default-on).
- New `make helm-crds` target syncs CRDs from `config/crd/bases/` to both chart copies; CI gates drift via `git diff --exit-code charts/`.
- New CI jobs: helm-lint + dry-run apply + drift gate (`lint.yml`); chart install on kind + Tunnel-Ready smoke (`e2e.yml`); helm package + OCI push to `ghcr.io/mtaku3/charts` on `v*` tags (`image.yml`).

## Test plan
- [x] `helm lint` clean for both charts
- [x] `helm template | kubectl apply --dry-run=client` clean for both charts
- [x] `make helm-crds && git diff --exit-code charts/` exits clean
- [ ] CI helm-lint passes
- [ ] CI e2e: helm install on kind reaches Tunnel `Ready`
- [ ] Tag a `v0.0.1-rc1`, confirm helm-publish job pushes both charts to GHCR

## Closes
Supersedes #12.

## Spec
docs/superpowers/specs/2026-05-06-helm-chart-design.md
EOF
)"
```

Expected: PR URL printed.

---

## Self-review (controller pre-flight)

**Spec coverage:**
- §"Repo layout" → Tasks 4, 5 (chart skeletons), Tasks 6–16 (templates).
- §"Chart metadata" → Tasks 4, 5.
- §"Values surface" → Tasks 4 (CRD chart values), 5 (operator chart values).
- §"Template behavior" — `_helpers.tpl` Task 6, Deployment Task 10, RBAC Tasks 8/9, Metrics Tasks 11/12, NetworkPolicy Task 13, PDB Task 14, HPA Task 15, NOTES Task 16.
- §"CRD generation pipeline" → Tasks 2 (wrap script), 3 (Makefile), 4/5 (initial generation).
- §"CI" → Tasks 17 (lint), 18 (e2e).
- §"Release" → Task 19.
- §"Migration" → Task 1 (close PR #12).
- §"Decisions log" → reflected in values defaults (Task 5) and template behavior tasks.

**Type / name consistency:**
- Chart name `frp-operator` consistent across all helper templates and template names.
- `selectorLabels` referenced from Deployment, ClusterRoleBinding (subjects), Service, ServiceMonitor, NetworkPolicy, PDB, HPA — all match the `_helpers.tpl` definition.
- `frp-operator.fullname` used as base for ClusterRole, RoleBinding, Service, ServiceMonitor, NetworkPolicy, PDB, HPA, Deployment — consistent suffix conventions (`-manager`, `-leader-election`, `-metrics`, base name).
- `leaseNamespace` helper used by Role/RoleBinding (Task 9) and Deployment `--leader-elect-namespace` arg (Task 10) — consistent.

**Placeholders:** none. All test commands have explicit `Expected:` lines. All template bodies are complete YAML.
