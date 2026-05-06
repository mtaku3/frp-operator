# Phase 1: API Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add new Karpenter-style CRD types (`ExitPool`, `ExitClaim`, `LocalDockerProviderClass`, `DigitalOceanProviderClass`) and replace `Tunnel` schema. Old `ExitServer` / `SchedulingPolicy` types and the controllers that depend on them are deleted in this phase too — clean break, no dual-support code paths.

**Architecture:** All new core types live in `api/v1alpha1/`. Each per-provider `ProviderClass` lives under its provider package (`pkg/cloudprovider/<provider>/v1alpha1/`). Shared types (FrpsConfig, NodeSelectorRequirementWithMinValues, ResourceRequirements, etc.) live in `api/v1alpha1/types_shared.go`. Controllers are deleted (entire `internal/controller/` directory removed) — Phase 2-9 rebuild them under `pkg/controllers/`. Tests for old controllers are also deleted.

**Tech Stack:** Go, kubebuilder controller-gen v0.20.1, sigs.k8s.io/controller-runtime, k8s.io/apimachinery, k8s.io/api.

**Spec section reference:** §3 (CRD specifications), §11 (feature gates — fields only).

**End state of this phase:**
- `go build ./...` succeeds.
- `go vet ./...` succeeds.
- `make manifests` produces CRD YAMLs for all new kinds.
- `make generate` runs controller-gen successfully (deepcopy methods regenerated).
- `make test` passes for the API package only (controllers gone). Webhook tests gone.
- The operator binary fails to start (no controllers wired) — that's fine; Phase 9 wires it back up.

---

## File map

### Created

```
api/v1alpha1/
├── exitpool_types.go                    # ExitPool, ExitPoolSpec, ExitPoolStatus, ExitClaimTemplate, Limits, Disruption, DisruptionBudget
├── exitclaim_types.go                   # ExitClaim, ExitClaimSpec, ExitClaimStatus
├── tunnel_types.go                      # REPLACED: Tunnel, TunnelSpec, TunnelStatus, TunnelPort
├── types_shared.go                      # FrpsConfig, FrpsAuthConfig, FrpsTLSConfig, NodeSelectorRequirementWithMinValues, ResourceRequirements, ProviderClassRef, SecretKeyRef, LocalObjectReference, Duration
├── conditions.go                        # Condition type constants (Launched, Registered, Initialized, Ready, Drifted, Empty, Disrupted, Expired, Consolidatable, ConsistentStateFound; plus Reason constants)
├── labels.go                            # Well-known label/annotation keys
├── groupversion_info.go                 # (already exists) — register all new kinds
└── zz_generated.deepcopy.go             # (codegen) — regenerated

pkg/cloudprovider/localdocker/v1alpha1/
├── localdockerproviderclass_types.go    # LocalDockerProviderClass, Spec, Status
├── groupversion_info.go                 # group: frp.operator.io, kind under sub-API or shared
└── zz_generated.deepcopy.go             # (codegen)

pkg/cloudprovider/digitalocean/v1alpha1/
├── digitaloceanproviderclass_types.go   # DigitalOceanProviderClass, Spec, Status
├── groupversion_info.go
└── zz_generated.deepcopy.go             # (codegen)

config/crd/bases/
├── frp.operator.io_exitpools.yaml       # (codegen)
├── frp.operator.io_exitclaims.yaml      # (codegen)
├── frp.operator.io_tunnels.yaml         # (codegen)
├── frp.operator.io_localdockerproviderclasses.yaml
└── frp.operator.io_digitaloceanproviderclasses.yaml
```

### Deleted

```
api/v1alpha1/exitserver_types.go
api/v1alpha1/exitserver_types_test.go
api/v1alpha1/schedulingpolicy_types.go        (if exists — confirm path during Task 0)
internal/controller/                           (whole directory)
internal/scheduler/                            (whole directory; rebuilt under pkg/controllers/provisioning/scheduling/ in Phase 4)
internal/webhook/                              (whole directory; webhooks dropped per spec §10)
pkg/certs/                                     (no longer needed; webhook server gone)
config/webhook/                                (kustomize directory)
config/certmanager/                            (already gone in current main, but verify)
config/crd/bases/frp.operator.io_exitservers.yaml
config/crd/bases/frp.operator.io_schedulingpolicies.yaml
cmd/manager/main.go                            (replaced with stub in Task 9; full re-wire in Phase 9)
internal/configuration/                        (delete; reused under pkg/operator in Phase 9)
internal/provider/                             (delete; reused under pkg/cloudprovider in Phase 2)
internal/frp/                                  (delete; reused under pkg/cloudprovider/frp in Phase 2)
test/e2e/                                      (delete; rebuilt in Phase 10)
test/utils/                                    (delete; rebuilt in Phase 10)
```

### Modified

```
PROJECT                                         # kubebuilder marks for new resources
Makefile                                        # update test target if needed
go.mod                                          # potential dep churn (most stays)
config/default/kustomization.yaml               # remove webhook ref, point to new CRDs
config/manager/manager.yaml                     # remove webhook port + cert volume
config/rbac/role.yaml                           # regenerate after kubebuilder markers
```

---

## Task 0: Pre-flight — confirm baseline

**Files:**
- Read: `api/v1alpha1/`, `internal/controller/`, `internal/scheduler/`, `internal/webhook/`, `cmd/manager/main.go`

- [ ] **Step 1: Snapshot current API types**

```bash
ls api/v1alpha1/*.go > /tmp/phase1-baseline-api.txt
ls internal/controller/*.go > /tmp/phase1-baseline-controllers.txt
ls internal/webhook/v1alpha1/*.go > /tmp/phase1-baseline-webhooks.txt
ls config/crd/bases/*.yaml > /tmp/phase1-baseline-crds.txt
```
Expected: file lists captured.

- [ ] **Step 2: Verify go build is currently green**

Run: `go build ./...`
Expected: exit 0, no output. (If fails, fix before proceeding — this phase isn't the place to debug pre-existing breakage.)

- [ ] **Step 3: Verify make test is currently green**

Run: `make test`
Expected: all packages PASS.

- [ ] **Step 4: Note current go.mod**

Run: `go mod tidy && git status -s go.mod go.sum`
Expected: no diff, or only whitespace. If diff, commit it as a separate baseline commit before starting Phase 1.

- [ ] **Step 5: No commit yet** — these are read-only checks.

---

## Task 1: Demolition — delete old subsystems

**Files:**
- Delete: `api/v1alpha1/exitserver_types.go`, `api/v1alpha1/exitserver_types_test.go`
- Delete: `api/v1alpha1/schedulingpolicy_types.go` (if present; confirm via Task 0)
- Delete: `internal/controller/` (recursive)
- Delete: `internal/scheduler/` (recursive)
- Delete: `internal/webhook/` (recursive)
- Delete: `internal/provider/` (recursive)
- Delete: `internal/frp/` (recursive)
- Delete: `internal/configuration/` (recursive)
- Delete: `internal/scheme/` (recursive)
- Delete: `pkg/certs/` (recursive)
- Delete: `pkg/utils/` (recursive — verify nothing in cmd/ depends on it; if it does, port to pkg/operator/ in Phase 9)
- Delete: `config/webhook/` (recursive)
- Delete: `config/certmanager/` (verify present first)
- Delete: `config/crd/bases/frp.operator.io_exitservers.yaml`
- Delete: `config/crd/bases/frp.operator.io_schedulingpolicies.yaml`
- Delete: `test/e2e/` (recursive)
- Delete: `test/utils/` (recursive)
- Delete: `config/overlays/e2e/` (recursive — webhook-related; rebuilt in Phase 10)
- Modify: `cmd/manager/main.go` → stub (see Step 4)
- Modify: `api/v1alpha1/groupversion_info.go` → drop SchemeBuilder registrations for deleted kinds
- Modify: `Makefile` → drop `test-e2e` until Phase 10 rebuilds it (replace with `@echo "test-e2e disabled until Phase 10"; exit 1`)

- [ ] **Step 1: rm the directories and files**

```bash
git rm -r internal/controller internal/scheduler internal/webhook internal/provider internal/frp internal/configuration internal/scheme
git rm -r pkg/certs
[ -d pkg/utils ] && git rm -r pkg/utils
git rm -r test/e2e test/utils
git rm -r config/webhook
[ -d config/certmanager ] && git rm -r config/certmanager
[ -d config/overlays/e2e ] && git rm -r config/overlays/e2e
git rm api/v1alpha1/exitserver_types.go api/v1alpha1/exitserver_types_test.go
[ -f api/v1alpha1/schedulingpolicy_types.go ] && git rm api/v1alpha1/schedulingpolicy_types.go
git rm config/crd/bases/frp.operator.io_exitservers.yaml
git rm config/crd/bases/frp.operator.io_schedulingpolicies.yaml
```

- [ ] **Step 2: Stub `cmd/manager/main.go`**

Replace contents entirely with:
```go
package main

import (
	"fmt"
	"os"
)

// Phase 1 stub. Real wiring restored in Phase 9.
func main() {
	fmt.Fprintln(os.Stderr, "frp-operator: refactor in progress; binary unwired until Phase 9")
	os.Exit(1)
}
```

- [ ] **Step 3: Edit `api/v1alpha1/groupversion_info.go`**

Open the file. Find the SchemeBuilder registrations near the bottom. Strip any lines registering `ExitServer{}`, `ExitServerList{}`, `SchedulingPolicy{}`, `SchedulingPolicyList{}`. Keep `Tunnel{}` and `TunnelList{}` registrations (Tunnel is replaced not deleted; the schema change happens in Task 4).

- [ ] **Step 4: Edit `Makefile`**

Find the `test-e2e:` target. Replace its body with:
```
test-e2e:
	@echo "test-e2e is disabled while Phase 1-9 land; see docs/superpowers/plans/2026-05-04-phase-10-e2e.md"
	@exit 1
```
Leave `test:`, `lint:`, `manifests:`, `generate:`, `docker-build:` etc. intact.

- [ ] **Step 5: Run go build**

Run: `go build ./...`
Expected: FAILS — `Tunnel` references gone types (`SchedulingPolicy`, etc.) inside `tunnel_types.go`. That's expected; Task 4 replaces tunnel_types.go.

We accept the temporary breakage; the next tasks fix it before commit.

- [ ] **Step 6: Do NOT commit yet.** Demolition is staged but not yet useful. Tasks 2-9 produce the new types that restore `go build`. Final commit at Task 10.

---

## Task 2: Shared types

**Files:**
- Create: `api/v1alpha1/types_shared.go`

- [ ] **Step 1: Write `types_shared.go`**

```go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License").
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Duration is a wrapper for metav1.Duration so callers don't need to import
// the metav1 package just for one field.
type Duration = metav1.Duration

// LocalObjectReference is a reference to a resource in the same namespace.
type LocalObjectReference struct {
	Name string `json:"name"`
}

// SecretKeyRef points at a key inside a Secret in the same namespace as the
// referencing object.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ProviderClassRef is a typed pointer to a per-provider config CRD.
// Mirrors Karpenter NodeClassReference{Group, Kind, Name}.
type ProviderClassRef struct {
	// Group is the API group of the ProviderClass kind, e.g. frp.operator.io.
	Group string `json:"group"`
	// Kind is the ProviderClass kind, e.g. LocalDockerProviderClass.
	Kind string `json:"kind"`
	// Name is the metadata.name of the ProviderClass instance.
	Name string `json:"name"`
}

// NodeSelectorOperator mirrors corev1.NodeSelectorOperator with explicit
// support for Karpenter-style operators (Gt, Lt) included.
type NodeSelectorOperator string

const (
	NodeSelectorOpIn           NodeSelectorOperator = "In"
	NodeSelectorOpNotIn        NodeSelectorOperator = "NotIn"
	NodeSelectorOpExists       NodeSelectorOperator = "Exists"
	NodeSelectorOpDoesNotExist NodeSelectorOperator = "DoesNotExist"
	NodeSelectorOpGt           NodeSelectorOperator = "Gt"
	NodeSelectorOpLt           NodeSelectorOperator = "Lt"
)

// NodeSelectorRequirementWithMinValues is the requirement struct used in
// ExitPool/ExitClaim/Tunnel requirements lists. It mirrors Karpenter's
// equivalent and adds the optional MinValues knob.
type NodeSelectorRequirementWithMinValues struct {
	// Key is the label/requirement key.
	Key string `json:"key"`
	// Operator is one of In, NotIn, Exists, DoesNotExist, Gt, Lt.
	Operator NodeSelectorOperator `json:"operator"`
	// Values is the operand list. Required for In, NotIn, Gt, Lt;
	// must be empty for Exists, DoesNotExist.
	// +optional
	Values []string `json:"values,omitempty"`
	// MinValues forces the scheduler to consider at least N distinct
	// values for this key when packing.
	// +optional
	MinValues *int `json:"minValues,omitempty"`
}

// ResourceRequirements is an extensible ResourceList wrapper. Same shape
// as Karpenter's NodeClaim.Spec.Resources.Requests and core
// Pod.Spec.Containers[].Resources.Requests. Recognized keys are
// documented in the spec.
type ResourceRequirements struct {
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// FrpsConfig is the full set of frps daemon options carried on
// ExitPool template + ExitClaim spec. Defaults applied by the
// scheduler/lifecycle controller when fields are omitted.
type FrpsConfig struct {
	// Version is the frps binary version, e.g. "v0.68.1". Drives image
	// tag (localdocker) or binary URL (DO cloud-init).
	Version string `json:"version"`
	// BindPort is the control-plane TCP port frpc connects to.
	// +kubebuilder:default=7000
	BindPort int32 `json:"bindPort,omitempty"`
	// AdminPort is the frps admin HTTP API port.
	// +kubebuilder:default=7400
	AdminPort int32 `json:"adminPort,omitempty"`
	// VhostHTTPPort is the optional HTTP vhost listener.
	// +optional
	VhostHTTPPort *int32 `json:"vhostHTTPPort,omitempty"`
	// VhostHTTPSPort is the optional HTTPS vhost listener.
	// +optional
	VhostHTTPSPort *int32 `json:"vhostHTTPSPort,omitempty"`
	// KCPBindPort is the optional KCP transport listener.
	// +optional
	KCPBindPort *int32 `json:"kcpBindPort,omitempty"`
	// QUICBindPort is the optional QUIC transport listener.
	// +optional
	QUICBindPort *int32 `json:"quicBindPort,omitempty"`
	// AllowPorts is the set of public port slots, e.g.
	// ["80","443","1024-65535"]. Scheduler binpacks port-conflicts here.
	AllowPorts []string `json:"allowPorts"`
	// ReservedPorts are subtracted from AllowPorts; frps internal/admin
	// ports auto-merged in by the scheduler.
	// +optional
	ReservedPorts []int32 `json:"reservedPorts,omitempty"`
	// Auth declares how frpc clients authenticate to frps.
	Auth FrpsAuthConfig `json:"auth"`
	// TLS pins the TLS material for the control plane.
	// +optional
	TLS *FrpsTLSConfig `json:"tls,omitempty"`
}

// FrpsAuthConfig declares the auth mode. v1 supports only token.
type FrpsAuthConfig struct {
	// Method selects the auth mode. Currently only "token" is implemented.
	// +kubebuilder:validation:Enum=token
	Method string `json:"method"`
	// TokenSecretRef references a Secret holding the shared token under
	// the named key. If unset, the operator generates a token at
	// provision time and writes a managed Secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// FrpsTLSConfig is the TLS material for the control plane.
type FrpsTLSConfig struct {
	// Force requires TLS for incoming frpc connections.
	// +optional
	Force bool `json:"force,omitempty"`
	// CertSecret references the server certificate.
	// +optional
	CertSecret *SecretKeyRef `json:"certSecret,omitempty"`
	// KeySecret references the server private key.
	// +optional
	KeySecret *SecretKeyRef `json:"keySecret,omitempty"`
	// CASecret enables mTLS by requiring clients to present a cert
	// signed by this CA.
	// +optional
	CASecret *SecretKeyRef `json:"caSecret,omitempty"`
}
```

- [ ] **Step 2: Verify file compiles in isolation**

Run: `go vet ./api/v1alpha1/...`
Expected: probably FAILS because deleted types are still referenced from `tunnel_types.go`. That's fine; Task 4 fixes.

- [ ] **Step 3: Don't commit yet.**

---

## Task 3: Conditions and well-known labels

**Files:**
- Create: `api/v1alpha1/conditions.go`
- Create: `api/v1alpha1/labels.go`

- [ ] **Step 1: Write `conditions.go`**

```go
package v1alpha1

// Condition types used across ExitClaim, Tunnel, ExitPool, and ProviderClass.
// Mirrors Karpenter's nodeclaim_status.go condition constants.
const (
	// Lifecycle (ExitClaim).
	ConditionTypeLaunched              = "Launched"
	ConditionTypeRegistered            = "Registered"
	ConditionTypeInitialized           = "Initialized"
	ConditionTypeReady                 = "Ready"
	ConditionTypeDrifted               = "Drifted"
	ConditionTypeEmpty                 = "Empty"
	ConditionTypeConsolidatable        = "Consolidatable"
	ConditionTypeDisrupted             = "Disrupted"
	ConditionTypeExpired               = "Expired"
	ConditionTypeConsistentStateFound  = "ConsistentStateFound"

	// Pool/ProviderClass status.
	ConditionTypeProviderClassReady = "ProviderClassReady"
	ConditionTypeValidationSucceeded = "ValidationSucceeded"
)

// Well-known reason codes paired with conditions.
const (
	ReasonProvisioning            = "Provisioning"
	ReasonProvisioned             = "Provisioned"
	ReasonProvisioningFailed      = "ProvisioningFailed"
	ReasonRegistrationTimeout     = "RegistrationTimeout"
	ReasonAdminAPIUnreachable     = "AdminAPIUnreachable"
	ReasonPortReservationFailed   = "PortReservationFailed"
	ReasonProviderError           = "ProviderError"
	ReasonProviderClassNotFound   = "ProviderClassNotFound"
	ReasonLimitsExceeded          = "LimitsExceeded"
	ReasonBudgetExceeded          = "BudgetExceeded"
	ReasonNoEligibleExit          = "NoEligibleExit"
	ReasonNoMatchingPool          = "NoMatchingPool"
	ReasonPortConflict            = "PortConflict"
	ReasonInvalidRequirements     = "InvalidRequirements"
	ReasonPoolHashMismatch        = "PoolHashMismatch"
	ReasonNotReady                = "NotReady"
	ReasonReconciled              = "Reconciled"
)
```

- [ ] **Step 2: Write `labels.go`**

```go
package v1alpha1

// Group is the API group used as the prefix for all domain labels and
// annotations.
const Group = "frp.operator.io"

// Well-known label keys.
const (
	// LabelExitPool stamps which ExitPool produced an ExitClaim. Used as
	// the dedup/idempotency key in the scheduler and for status rollup.
	LabelExitPool        = Group + "/exitpool"
	LabelProvider        = Group + "/provider"
	LabelRegion          = Group + "/region"
	LabelTier            = Group + "/tier"
	LabelCreatedForTunnel = Group + "/created-for-tunnel"
	LabelInitialized     = Group + "/initialized"
)

// Well-known annotation keys.
const (
	AnnotationDoNotDisrupt = Group + "/do-not-disrupt"
	AnnotationPoolHash     = Group + "/pool-hash"

	// ServiceWatcher annotations on Service for translation into Tunnel.Spec.
	AnnotationServiceCPURequest        = Group + "/resources.requests.cpu"
	AnnotationServiceMemoryRequest     = Group + "/resources.requests.memory"
	AnnotationServiceBandwidthRequest  = Group + "/resources.requests.bandwidthMbps"
	AnnotationServiceTrafficRequest    = Group + "/resources.requests.monthlyTrafficGB"
	AnnotationServiceRequirementsJSON  = Group + "/requirements"
	AnnotationServiceExitPool          = Group + "/exit-pool"
	AnnotationServiceExitClaimRef      = Group + "/exit-claim-ref"
	AnnotationServiceExpireAfter       = Group + "/expire-after"
)

// Finalizer string applied to ExitClaim. Mirrors Karpenter's single
// "karpenter.sh/termination" finalizer.
const TerminationFinalizer = Group + "/termination"

// Resource keys recognized in ResourceList fields. Standard k8s names
// (cpu, memory) need no constant. Domain-prefixed extended resources
// listed here.
const (
	ResourceExits            = Group + "/exits"
	ResourceBandwidthMbps    = Group + "/bandwidthMbps"
	ResourceMonthlyTrafficGB = Group + "/monthlyTrafficGB"
)
```

- [ ] **Step 3: Don't commit yet.**

---

## Task 4: Tunnel types (replace existing)

**Files:**
- Modify: `api/v1alpha1/tunnel_types.go` (full rewrite)

- [ ] **Step 1: Replace `tunnel_types.go` contents entirely**

```go
/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tn
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name=Exit,type=string,JSONPath=`.status.assignedExit`
// +kubebuilder:printcolumn:name=IP,type=string,JSONPath=`.status.assignedIP`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type Tunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TunnelSpec   `json:"spec,omitempty"`
	Status TunnelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tunnel `json:"items"`
}

type TunnelSpec struct {
	Ports        []TunnelPort                            `json:"ports"`
	Requirements []NodeSelectorRequirementWithMinValues  `json:"requirements,omitempty"`
	// ExitClaimRef hard-pins the tunnel to a specific ExitClaim, bypassing
	// the scheduler.
	// +optional
	ExitClaimRef *LocalObjectReference                  `json:"exitClaimRef,omitempty"`
	// Resources.Requests is the binpack input. Optional; empty fits anywhere.
	// +optional
	Resources    ResourceRequirements                    `json:"resources,omitempty"`
}

type TunnelPort struct {
	// Name is a free-form identifier; required when there is more than one port.
	// +optional
	Name string `json:"name,omitempty"`
	// PublicPort is the requested public port on the exit. 0 means
	// auto-assign from AllowPorts.
	// +optional
	PublicPort *int32 `json:"publicPort,omitempty"`
	// ServicePort is the in-cluster port frpc forwards traffic from.
	ServicePort int32 `json:"servicePort"`
	// Protocol is "TCP" (default) or "UDP".
	// +kubebuilder:default=TCP
	// +kubebuilder:validation:Enum=TCP;UDP
	Protocol string `json:"protocol,omitempty"`
}

type TunnelPhase string

const (
	TunnelPhasePending     TunnelPhase = ""
	TunnelPhaseAllocating  TunnelPhase = "Allocating"
	TunnelPhaseProvisioning TunnelPhase = "Provisioning"
	TunnelPhaseReady       TunnelPhase = "Ready"
	TunnelPhaseFailed      TunnelPhase = "Failed"
)

type TunnelStatus struct {
	// Phase is a coarse summary of Conditions.
	// +optional
	Phase         TunnelPhase        `json:"phase,omitempty"`
	// AssignedExit is the name of the ExitClaim this tunnel is bound to.
	// +optional
	AssignedExit  string             `json:"assignedExit,omitempty"`
	// AssignedIP is the public IP of the assigned ExitClaim.
	// +optional
	AssignedIP    string             `json:"assignedIP,omitempty"`
	// AssignedPorts are the resolved public port numbers (auto-assigned
	// values filled in for PublicPort=0 inputs).
	// +optional
	AssignedPorts []int32            `json:"assignedPorts,omitempty"`
	// +optional
	Conditions    []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Tunnel{}, &TunnelList{})
}
```

- [ ] **Step 2: Don't commit yet.**

---

## Task 5: ExitPool types

**Files:**
- Create: `api/v1alpha1/exitpool_types.go`

- [ ] **Step 1: Write `exitpool_types.go`**

```go
/*
Copyright 2026.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ep
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Exits,type=integer,JSONPath=`.status.exits`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type ExitPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExitPoolSpec   `json:"spec,omitempty"`
	Status ExitPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ExitPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExitPool `json:"items"`
}

type ExitPoolSpec struct {
	Template   ExitClaimTemplate `json:"template"`
	// +optional
	Disruption Disruption        `json:"disruption,omitempty"`
	// +optional
	Limits     Limits            `json:"limits,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Weight     *int32            `json:"weight,omitempty"`
	// Replicas enables static-mode (alpha, gated by feature gate
	// StaticReplicas). When set, the operator maintains exactly N
	// ExitClaims regardless of demand.
	// +optional
	Replicas   *int64            `json:"replicas,omitempty"`
}

type ExitClaimTemplate struct {
	// +optional
	Metadata ExitClaimTemplateMetadata `json:"metadata,omitempty"`
	Spec     ExitClaimTemplateSpec     `json:"spec"`
}

type ExitClaimTemplateMetadata struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ExitClaimTemplateSpec deliberately omits Resources — per Karpenter
// convention, users cannot pre-allocate per-claim resource requests on
// the pool template. Scheduler fills ExitClaim.Spec.Resources.Requests
// at provision time.
type ExitClaimTemplateSpec struct {
	ProviderClassRef       ProviderClassRef                       `json:"providerClassRef"`
	// +optional
	Requirements           []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
	Frps                   FrpsConfig                             `json:"frps"`
	// +optional
	ExpireAfter            Duration                               `json:"expireAfter,omitempty"`
	// +optional
	TerminationGracePeriod *Duration                              `json:"terminationGracePeriod,omitempty"`
}

type Disruption struct {
	// +kubebuilder:validation:Enum=WhenEmpty;WhenEmptyOrUnderutilized
	// +optional
	ConsolidationPolicy ConsolidationPolicy `json:"consolidationPolicy,omitempty"`
	// +optional
	ConsolidateAfter    Duration            `json:"consolidateAfter,omitempty"`
	// +optional
	Budgets             []DisruptionBudget  `json:"budgets,omitempty"`
}

type ConsolidationPolicy string

const (
	ConsolidationWhenEmpty               ConsolidationPolicy = "WhenEmpty"
	ConsolidationWhenEmptyOrUnderutilized ConsolidationPolicy = "WhenEmptyOrUnderutilized"
)

type DisruptionReason string

const (
	DisruptionReasonEmpty          DisruptionReason = "Empty"
	DisruptionReasonDrifted        DisruptionReason = "Drifted"
	DisruptionReasonExpired        DisruptionReason = "Expired"
	DisruptionReasonUnderutilized  DisruptionReason = "Underutilized"
)

type DisruptionBudget struct {
	// Nodes is "10%" or "5"; max disruptions allowed concurrently.
	Nodes string `json:"nodes"`
	// +optional
	Schedule string `json:"schedule,omitempty"`
	// +optional
	Duration *Duration `json:"duration,omitempty"`
	// +optional
	Reasons []DisruptionReason `json:"reasons,omitempty"`
}

// Limits is an extensible ResourceList ceiling. Recognized keys:
//   cpu, memory                                standard k8s names
//   frp.operator.io/exits                      count of ExitClaims
//   frp.operator.io/bandwidthMbps              aggregate bandwidth
//   frp.operator.io/monthlyTrafficGB           aggregate traffic budget
type Limits corev1.ResourceList

type ExitPoolStatus struct {
	// +optional
	Conditions []metav1.Condition  `json:"conditions,omitempty"`
	// +optional
	Exits      int64               `json:"exits"`
	// +optional
	Resources  corev1.ResourceList `json:"resources,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ExitPool{}, &ExitPoolList{})
}
```

- [ ] **Step 2: Don't commit yet.**

---

## Task 6: ExitClaim types

**Files:**
- Create: `api/v1alpha1/exitclaim_types.go`

- [ ] **Step 1: Write `exitclaim_types.go`**

```go
/*
Copyright 2026.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ec
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Pool,type=string,JSONPath=`.metadata.labels.frp\.operator\.io/exitpool`
// +kubebuilder:printcolumn:name=Provider,type=string,JSONPath=`.spec.providerClassRef.kind`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=PublicIP,type=string,JSONPath=`.status.publicIP`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type ExitClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExitClaimSpec   `json:"spec,omitempty"`
	Status ExitClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ExitClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExitClaim `json:"items"`
}

type ExitClaimSpec struct {
	ProviderClassRef       ProviderClassRef                       `json:"providerClassRef"`
	// +optional
	Requirements           []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
	Frps                   FrpsConfig                             `json:"frps"`
	// Resources.Requests is controller-derived: sum of bound tunnel
	// requests + frps overhead. Users do NOT set this directly.
	// +optional
	Resources              ResourceRequirements                   `json:"resources,omitempty"`
	// +optional
	ExpireAfter            Duration                               `json:"expireAfter,omitempty"`
	// +optional
	TerminationGracePeriod *Duration                              `json:"terminationGracePeriod,omitempty"`
}

type ExitClaimStatus struct {
	// ProviderID is the cloud-provider identifier (e.g.
	// localdocker://<container-id>, do://<droplet-id>).
	// +optional
	ProviderID  string             `json:"providerID,omitempty"`
	// +optional
	PublicIP    string             `json:"publicIP,omitempty"`
	// ExitName is the provider-side resource name (container, droplet).
	// +optional
	ExitName    string             `json:"exitName,omitempty"`
	// ImageID is the provider's image identifier (container digest,
	// droplet base image). Mirrors Karpenter NodeClaim.Status.ImageID.
	// +optional
	ImageID     string             `json:"imageID,omitempty"`
	// FrpsVersion is the actually-running frps binary version, reported
	// by admin API after Registration.
	// +optional
	FrpsVersion string             `json:"frpsVersion,omitempty"`
	// Capacity is the estimated full capacity of the exit. Populated
	// from cloudProvider.Create return value. ESTIMATE only — same
	// convention as Karpenter NodeClaim.Status.Capacity.
	// +optional
	Capacity    corev1.ResourceList `json:"capacity,omitempty"`
	// Allocatable is the estimated allocatable capacity.
	// +optional
	Allocatable corev1.ResourceList `json:"allocatable,omitempty"`
	// +optional
	Conditions  []metav1.Condition  `json:"conditions,omitempty"`

	// NOTE: there is no Allocations field. Truth lives on each Tunnel's
	// Status.AssignedExit + Status.AssignedPorts. state.StateExit
	// aggregates them in-memory.
}

func init() {
	SchemeBuilder.Register(&ExitClaim{}, &ExitClaimList{})
}
```

- [ ] **Step 2: Don't commit yet.**

---

## Task 7: LocalDockerProviderClass types

**Files:**
- Create: `pkg/cloudprovider/localdocker/v1alpha1/localdockerproviderclass_types.go`
- Create: `pkg/cloudprovider/localdocker/v1alpha1/groupversion_info.go`
- Create: `pkg/cloudprovider/localdocker/v1alpha1/doc.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package v1alpha1 contains the LocalDockerProviderClass API types.
// +kubebuilder:object:generate=true
// +groupName=frp.operator.io
package v1alpha1
```

- [ ] **Step 2: Write `groupversion_info.go`**

```go
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{Group: "frp.operator.io", Version: "v1alpha1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)
```

- [ ] **Step 3: Write `localdockerproviderclass_types.go`**

```go
/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ldpc
// +kubebuilder:subresource:status
type LocalDockerProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LocalDockerProviderClassSpec   `json:"spec,omitempty"`
	Status LocalDockerProviderClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type LocalDockerProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LocalDockerProviderClass `json:"items"`
}

type LocalDockerProviderClassSpec struct {
	// Network is the docker network name; localdocker exits attach here.
	// +kubebuilder:default=kind
	Network string `json:"network,omitempty"`
	// ConfigHostMountPath is the host directory bind-mounted into each
	// frps container so the operator can write frps.toml. Must exist
	// and be world-writable on every kind node.
	// +kubebuilder:default="/tmp/frp-operator-shared"
	ConfigHostMountPath string `json:"configHostMountPath,omitempty"`
	// ImagePullPolicy controls whether containers always re-pull.
	// +kubebuilder:default=IfNotPresent
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
	// SkipHostPortPublishing disables -p hostPort:containerPort flags;
	// useful for kind e2e where multiple exits share host ports.
	// +optional
	SkipHostPortPublishing bool `json:"skipHostPortPublishing,omitempty"`
	// DefaultImage is the base frps container image template, e.g.
	// "fatedier/frps:%s" — version substituted at launch.
	// +kubebuilder:default="fatedier/frps:%s"
	DefaultImage string `json:"defaultImage,omitempty"`
}

type LocalDockerProviderClassStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&LocalDockerProviderClass{}, &LocalDockerProviderClassList{})
}
```

- [ ] **Step 4: Don't commit yet.**

---

## Task 8: DigitalOceanProviderClass types

**Files:**
- Create: `pkg/cloudprovider/digitalocean/v1alpha1/digitaloceanproviderclass_types.go`
- Create: `pkg/cloudprovider/digitalocean/v1alpha1/groupversion_info.go`
- Create: `pkg/cloudprovider/digitalocean/v1alpha1/doc.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package v1alpha1 contains the DigitalOceanProviderClass API types.
// +kubebuilder:object:generate=true
// +groupName=frp.operator.io
package v1alpha1
```

- [ ] **Step 2: Write `groupversion_info.go`**

```go
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{Group: "frp.operator.io", Version: "v1alpha1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)
```

- [ ] **Step 3: Write `digitaloceanproviderclass_types.go`**

```go
/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=dopc
// +kubebuilder:subresource:status
type DigitalOceanProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DigitalOceanProviderClassSpec   `json:"spec,omitempty"`
	Status DigitalOceanProviderClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DigitalOceanProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DigitalOceanProviderClass `json:"items"`
}

type DigitalOceanProviderClassSpec struct {
	// APITokenSecretRef references the Secret holding a DO API token.
	APITokenSecretRef frpv1alpha1.SecretKeyRef `json:"apiTokenSecretRef"`
	// Region is the DO region slug (nyc3, sfo3, ...).
	Region string `json:"region"`
	// Size is the droplet size slug (s-1vcpu-1gb, ...).
	Size string `json:"size"`
	// ImageID is the droplet base image; default ubuntu-22-04-x64.
	// +kubebuilder:default="ubuntu-22-04-x64"
	ImageID string `json:"imageID,omitempty"`
	// VPCUUID optionally pins the VPC.
	// +optional
	VPCUUID string `json:"vpcUUID,omitempty"`
	// SSHKeyIDs lists DO ssh key IDs to inject.
	// +optional
	SSHKeyIDs []string `json:"sshKeyIDs,omitempty"`
	// Monitoring enables DO monitoring agent.
	// +optional
	Monitoring bool `json:"monitoring,omitempty"`
	// DefaultImage is the frps binary version pulled in cloud-init,
	// templated as "%s" -> version.
	// +kubebuilder:default="fatedier/frps:%s"
	DefaultImage string `json:"defaultImage,omitempty"`
}

type DigitalOceanProviderClassStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&DigitalOceanProviderClass{}, &DigitalOceanProviderClassList{})
}
```

- [ ] **Step 4: Don't commit yet.**

---

## Task 9: Update PROJECT and run codegen

**Files:**
- Modify: `PROJECT` (kubebuilder marker file)
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`
- Generated: `pkg/cloudprovider/localdocker/v1alpha1/zz_generated.deepcopy.go`
- Generated: `pkg/cloudprovider/digitalocean/v1alpha1/zz_generated.deepcopy.go`
- Generated: `config/crd/bases/*.yaml`

- [ ] **Step 1: Update PROJECT file**

Open `PROJECT`. Find the `resources:` list. Replace any `kind: ExitServer` / `kind: SchedulingPolicy` entries with:

```yaml
- api:
    crdVersion: v1
    namespaced: false
  controller: false
  domain: operator.io
  group: frp
  kind: ExitPool
  path: github.com/mtaku3/frp-operator/api/v1alpha1
  version: v1alpha1
- api:
    crdVersion: v1
    namespaced: false
  controller: false
  domain: operator.io
  group: frp
  kind: ExitClaim
  path: github.com/mtaku3/frp-operator/api/v1alpha1
  version: v1alpha1
- api:
    crdVersion: v1
    namespaced: false
  controller: false
  domain: operator.io
  group: frp
  kind: LocalDockerProviderClass
  path: github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1
  version: v1alpha1
- api:
    crdVersion: v1
    namespaced: false
  controller: false
  domain: operator.io
  group: frp
  kind: DigitalOceanProviderClass
  path: github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1
  version: v1alpha1
```

Keep the existing Tunnel entry (schema replaced, not the entry).

- [ ] **Step 2: Run controller-gen for deepcopy**

Run: `make generate`
Expected: regenerates `api/v1alpha1/zz_generated.deepcopy.go` with new method sets, and creates the same file under each provider package's v1alpha1.

If `make generate` doesn't traverse the new provider packages, edit the Makefile target to include them. The current target likely says:
```
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
```
That `./...` already covers them.

- [ ] **Step 3: Run controller-gen for CRDs**

Run: `make manifests`
Expected: writes `config/crd/bases/frp.operator.io_exitpools.yaml`, `..._exitclaims.yaml`, `..._tunnels.yaml`, `..._localdockerproviderclasses.yaml`, `..._digitaloceanproviderclasses.yaml`.

- [ ] **Step 4: Verify CRDs render**

Run: `cat config/crd/bases/frp.operator.io_exitpools.yaml | head -40`
Expected: valid YAML with `kind: CustomResourceDefinition`, `spec.names.kind: ExitPool`, scope Cluster.

Repeat for each kind.

- [ ] **Step 5: Update kustomization to install all five CRDs**

Edit `config/crd/kustomization.yaml`. Find the `resources:` list. Ensure it lists exactly:
```yaml
resources:
- bases/frp.operator.io_exitpools.yaml
- bases/frp.operator.io_exitclaims.yaml
- bases/frp.operator.io_tunnels.yaml
- bases/frp.operator.io_localdockerproviderclasses.yaml
- bases/frp.operator.io_digitaloceanproviderclasses.yaml
```
Remove any reference to `frp.operator.io_exitservers.yaml` or `frp.operator.io_schedulingpolicies.yaml`.

- [ ] **Step 6: Don't commit yet.**

---

## Task 10: Verify build, vet, and CRD installation

**Files:** none modified.

- [ ] **Step 1: Run go build**

Run: `go build ./...`
Expected: exit 0, no errors. (cmd/manager/main.go is the stub — that builds.)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0, no errors.

- [ ] **Step 3: Run go test**

Run: `go test ./api/... ./pkg/cloudprovider/localdocker/v1alpha1/... ./pkg/cloudprovider/digitalocean/v1alpha1/...`
Expected: PASS (no tests yet, so "no test files" or empty pass).

- [ ] **Step 4: Run make manifests; verify no diff**

Run: `make manifests && git status -s config/crd/`
Expected: no diff (re-running codegen shouldn't change anything once stable).

- [ ] **Step 5: Run make generate; verify no diff**

Run: `make generate && git status -s api/ pkg/`
Expected: no diff.

- [ ] **Step 6: Smoke-test CRD install via envtest setup**

Write a throwaway test at `api/v1alpha1/install_test.go`:

```go
//go:build !skip_envtest

package v1alpha1_test

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

func TestCRDsInstall(t *testing.T) {
	if err := frpv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("frp v1alpha1 AddToScheme: %v", err)
	}
	if err := ldv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("localdocker v1alpha1 AddToScheme: %v", err)
	}
	if err := dov1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("digitalocean v1alpha1 AddToScheme: %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = env.Stop() }()
	if cfg == nil {
		t.Fatal("envtest returned nil cfg")
	}
}
```

Run: `KUBEBUILDER_ASSETS=$(setup-envtest use 1.31.x -p path) go test ./api/v1alpha1/... -run TestCRDsInstall -v`

Expected: PASS within ~10s. If envtest binaries not installed locally, run `make envtest` (or `make test` which usually pulls them) to fetch.

- [ ] **Step 7: Delete the throwaway test**

```bash
rm api/v1alpha1/install_test.go
```

(We will write proper installation tests as part of state.Cluster suite_test.go in Phase 3. The throwaway test was only to validate Phase 1 codegen output.)

---

## Task 11: Commit Phase 1

**Files:** none additional; staging the work from Tasks 1-10.

- [ ] **Step 1: Stage everything**

```bash
git add -A
git status -s | head -30
```
Expected: ~30+ files staged: deletions of internal/, additions of api/v1alpha1/exit{pool,claim}_types.go and related, generated manifests under config/crd/bases.

- [ ] **Step 2: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(api): phase 1 — karpenter-style CRD foundations

Replace ExitServer/SchedulingPolicy with ExitPool, ExitClaim, plus
per-provider LocalDockerProviderClass and DigitalOceanProviderClass.
Tunnel schema rewritten to match new ResourceRequirements shape and
NodeSelectorRequirementWithMinValues.

Demolition: deleted internal/controller, internal/scheduler,
internal/webhook, internal/provider, internal/frp, pkg/certs,
config/webhook, config/certmanager. cmd/manager/main.go stubbed
until Phase 9 wires the operator back up.

Generated:
- api/v1alpha1/zz_generated.deepcopy.go
- pkg/cloudprovider/{localdocker,digitalocean}/v1alpha1/zz_generated.deepcopy.go
- config/crd/bases/frp.operator.io_{exitpools,exitclaims,tunnels,localdockerproviderclasses,digitaloceanproviderclasses}.yaml

Phase 2 ports cloudprovider implementations against the new
provider-class types. Phase 9 restores the manager binary. Phase 10
rebuilds e2e against the new CRD shapes.

Spec: docs/superpowers/specs/2026-05-04-karpenter-style-refactor-design.md
Master plan: docs/superpowers/plans/2026-05-04-karpenter-refactor-master.md
EOF
)"
```

- [ ] **Step 2: Verify clean state**

Run: `git status`
Expected: `nothing to commit, working tree clean`.

- [ ] **Step 3: Verify build still green**

Run: `go build ./... && go vet ./... && go test ./api/...`
Expected: all green.

---

## Phase 1 acceptance checklist

- [x] Old API kinds (ExitServer, SchedulingPolicy) removed from `api/v1alpha1/`.
- [x] Old controllers, webhooks, certs, providers, e2e all removed (rebuilt in later phases).
- [x] New API kinds present: ExitPool, ExitClaim, Tunnel (rewritten), LocalDockerProviderClass, DigitalOceanProviderClass.
- [x] Shared types live in `api/v1alpha1/types_shared.go`: FrpsConfig, FrpsAuthConfig, FrpsTLSConfig, NodeSelectorRequirementWithMinValues, ResourceRequirements, ProviderClassRef, SecretKeyRef, LocalObjectReference, Duration, NodeSelectorOperator constants.
- [x] Conditions and labels constants live in `api/v1alpha1/conditions.go` and `api/v1alpha1/labels.go`.
- [x] `make generate` produces clean diff (deepcopy regenerated).
- [x] `make manifests` produces five CRD YAMLs in `config/crd/bases/`.
- [x] `config/crd/kustomization.yaml` lists the five CRDs.
- [x] `cmd/manager/main.go` stubbed (manager binary unwired).
- [x] `go build ./...` succeeds.
- [x] `go vet ./...` succeeds.
- [x] `go test ./api/...` succeeds (only deepcopy + struct types, no behavior tests yet).
- [x] envtest smoke test installs all five CRDs (test deleted after verification).
- [x] One commit on the `karpenter-refactor` branch.

---

## Out-of-scope reminders for Phase 1

These belong to later phases. If a subagent is tempted to do them, redirect:

- Webhook server, CEL validation, conversion webhooks → Phase 9.
- Field indexers, manager wiring, leader election → Phase 9.
- CloudProvider interface implementation, fake provider → Phase 2.
- Scheduler, batcher, provisioner → Phase 4.
- Lifecycle controller → Phase 5.
- Disruption controller, methods, budgets → Phase 6.
- ExitPool counter/hash/validation/readiness → Phase 7.
- ServiceWatcher → Phase 8.
- E2E suite → Phase 10.

**Do not** add CRD validation webhooks. CEL on the CRD is the only validation surface. Don't create `internal/webhook/` again.

**Do not** add conversion webhooks. The new API is `v1alpha1` from scratch; there is no `v1beta1` to convert to/from yet.

**Do not** copy code from `internal/provider/` into `pkg/cloudprovider/`. Phase 2 rewrites it against the new CloudProvider interface — direct copies will mislead.
