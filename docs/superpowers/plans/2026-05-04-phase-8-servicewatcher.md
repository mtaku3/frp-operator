# Phase 8: ServiceWatcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Translate `Service` of `loadBalancerClass=frp-operator.io/frp` into a sibling `Tunnel` CR. Service annotations carry tunnel-level config (resources, requirements, exit-pool selector, do-not-disrupt). The Tunnel's lifecycle is owned by user-facing controllers (Phases 4-6); ServiceWatcher only does the translation.

**Architecture:** One controller watching `corev1.Service`. On reconcile: ignore Services without our `loadBalancerClass`; otherwise read annotations, build a Tunnel object, server-side-apply it. On Service delete (DeletionTimestamp set): delete the sibling Tunnel. On Service.Status update from elsewhere (reverse sync), no-op. Reverse sync — populating `Service.Status.LoadBalancer.Ingress[0].IP` from `Tunnel.Status.AssignedIP` — is a SEPARATE small controller in this phase.

**Spec:** §3.4 (Tunnel surface), `Tunnel` annotation contract; relevant sections of design doc on Service annotations.

**Prerequisites:** Phases 1-4 (Tunnel CR exists, scheduler picks them up).

**End state:**
- `pkg/controllers/servicewatcher/` translates Service ↔ Tunnel + reverse-syncs status.
- envtest suite: create Service, sibling Tunnel appears with correct annotations + Spec; delete Service removes Tunnel; setting Tunnel.Status.AssignedIP populates Service.Status.LoadBalancer.Ingress.
- `make test` passes.

---

## File map

```
pkg/controllers/servicewatcher/
├── doc.go
├── controller.go                        # Service → Tunnel translation
├── annotations.go                       # parseAnnotations: Service.Annotations → TunnelSpec fields
├── annotations_test.go                  # unit: full annotation parsing matrix
├── reverse_sync.go                      # Tunnel → Service.Status.LoadBalancer.Ingress
├── controller_test.go                   # envtest: full Create / Delete / status round-trip
└── suite_test.go
```

---

## Task 1: annotations.go (pure function)

```go
package servicewatcher

import (
    "encoding/json"
    "fmt"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/resource"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ParseAnnotations reads the frp.operator.io/* annotations off a Service
// and returns the partial TunnelSpec they describe.
func ParseAnnotations(svc *corev1.Service) (v1alpha1.TunnelSpec, error) {
    spec := v1alpha1.TunnelSpec{}
    a := svc.Annotations

    // Resources
    if v := a[v1alpha1.AnnotationServiceCPURequest]; v != "" {
        q, err := resource.ParseQuantity(v); if err != nil { return spec, fmt.Errorf("cpu: %w", err) }
        if spec.Resources.Requests == nil { spec.Resources.Requests = corev1.ResourceList{} }
        spec.Resources.Requests[corev1.ResourceCPU] = q
    }
    if v := a[v1alpha1.AnnotationServiceMemoryRequest]; v != "" {
        q, err := resource.ParseQuantity(v); if err != nil { return spec, fmt.Errorf("memory: %w", err) }
        if spec.Resources.Requests == nil { spec.Resources.Requests = corev1.ResourceList{} }
        spec.Resources.Requests[corev1.ResourceMemory] = q
    }
    if v := a[v1alpha1.AnnotationServiceBandwidthRequest]; v != "" {
        q, err := resource.ParseQuantity(v); if err != nil { return spec, fmt.Errorf("bandwidth: %w", err) }
        if spec.Resources.Requests == nil { spec.Resources.Requests = corev1.ResourceList{} }
        spec.Resources.Requests[corev1.ResourceName(v1alpha1.ResourceBandwidthMbps)] = q
    }
    if v := a[v1alpha1.AnnotationServiceTrafficRequest]; v != "" {
        q, err := resource.ParseQuantity(v); if err != nil { return spec, fmt.Errorf("traffic: %w", err) }
        if spec.Resources.Requests == nil { spec.Resources.Requests = corev1.ResourceList{} }
        spec.Resources.Requests[corev1.ResourceName(v1alpha1.ResourceMonthlyTrafficGB)] = q
    }

    // Requirements: JSON-encoded array.
    if v := a[v1alpha1.AnnotationServiceRequirementsJSON]; v != "" {
        if err := json.Unmarshal([]byte(v), &spec.Requirements); err != nil {
            return spec, fmt.Errorf("requirements: %w", err)
        }
    }

    // Exit-pool shorthand: maps to a requirements entry.
    if v := a[v1alpha1.AnnotationServiceExitPool]; v != "" {
        spec.Requirements = append(spec.Requirements, v1alpha1.NodeSelectorRequirementWithMinValues{
            Key: v1alpha1.LabelExitPool, Operator: v1alpha1.NodeSelectorOpIn, Values: []string{v},
        })
    }

    // Hard pin.
    if v := a[v1alpha1.AnnotationServiceExitClaimRef]; v != "" {
        spec.ExitClaimRef = &v1alpha1.LocalObjectReference{Name: v}
    }

    return spec, nil
}

// PortsFromService translates Service.Spec.Ports to TunnelSpec.Ports.
// Note: corev1.ServicePort.Port becomes TunnelPort.PublicPort (the public
// face). corev1.ServicePort.TargetPort becomes TunnelPort.ServicePort.
func PortsFromService(svc *corev1.Service) []v1alpha1.TunnelPort {
    out := make([]v1alpha1.TunnelPort, 0, len(svc.Spec.Ports))
    for _, p := range svc.Spec.Ports {
        targetPort := p.TargetPort.IntValue()
        if targetPort == 0 { targetPort = int(p.Port) } // Service port if no targetPort.
        protocol := string(p.Protocol)
        if protocol == "" { protocol = "TCP" }
        publicPort := p.Port
        out = append(out, v1alpha1.TunnelPort{
            Name:        p.Name,
            PublicPort:  &publicPort,
            ServicePort: int32(targetPort),
            Protocol:    protocol,
        })
    }
    return out
}
```

Tests: each annotation parses, malformed values bubble errors, JSON requirements round-trip, port translation handles named/numeric targetPort, missing protocol defaults TCP.

Commit: `feat(servicewatcher): annotation parser + Service→TunnelPort translation`.

---

## Task 2: controller.go (Service → Tunnel)

```go
package servicewatcher

import (
    "context"

    corev1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const LoadBalancerClass = v1alpha1.Group + "/frp"

type Controller struct{ Client client.Client }

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var svc corev1.Service
    if err := r.Client.Get(ctx, req.NamespacedName, &svc); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Filter: only act on LoadBalancer + our class.
    if svc.Spec.Type != corev1.ServiceTypeLoadBalancer { return ctrl.Result{}, nil }
    if svc.Spec.LoadBalancerClass == nil || *svc.Spec.LoadBalancerClass != LoadBalancerClass { return ctrl.Result{}, nil }

    if !svc.DeletionTimestamp.IsZero() {
        return r.deleteSibling(ctx, &svc)
    }

    return r.upsertSibling(ctx, &svc)
}

func (r *Controller) upsertSibling(ctx context.Context, svc *corev1.Service) (ctrl.Result, error) {
    spec, err := ParseAnnotations(svc)
    if err != nil { return ctrl.Result{}, err }
    spec.Ports = PortsFromService(svc)

    desired := &v1alpha1.Tunnel{
        ObjectMeta: metav1.ObjectMeta{
            Name:      svc.Name,
            Namespace: svc.Namespace,
            Labels:    map[string]string{v1alpha1.Group + "/managed-by-service": "true"},
            Annotations: copyDoNotDisrupt(svc.Annotations),
        },
        Spec: spec,
    }

    var existing v1alpha1.Tunnel
    if err := r.Client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &existing); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, r.Client.Create(ctx, desired)
        }
        return ctrl.Result{}, err
    }

    // Update if Spec differs.
    if !specEqual(existing.Spec, desired.Spec) || !annotationsEqual(existing.Annotations, desired.Annotations) {
        existing.Spec = desired.Spec
        existing.Annotations = desired.Annotations
        return ctrl.Result{}, r.Client.Update(ctx, &existing)
    }
    return ctrl.Result{}, nil
}

func (r *Controller) deleteSibling(ctx context.Context, svc *corev1.Service) (ctrl.Result, error) {
    var t v1alpha1.Tunnel
    if err := r.Client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &t); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    return ctrl.Result{}, r.Client.Delete(ctx, &t)
}

func copyDoNotDisrupt(a map[string]string) map[string]string {
    if v := a[v1alpha1.AnnotationDoNotDisrupt]; v != "" {
        return map[string]string{v1alpha1.AnnotationDoNotDisrupt: v}
    }
    return nil
}

func specEqual(a, b v1alpha1.TunnelSpec) bool { /* DeepEqual */ }
func annotationsEqual(a, b map[string]string) bool { /* DeepEqual */ }

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).Named("servicewatcher").For(&corev1.Service{}).Complete(r)
}
```

Tests: create LB Service of our class → sibling Tunnel; create LB Service of other class → no Tunnel; update annotation → Tunnel updated; delete Service → Tunnel deleted.

Commit: `feat(servicewatcher): Service → Tunnel sibling translation`.

---

## Task 3: reverse_sync.go (Tunnel.Status → Service.Status)

```go
package servicewatcher

// ReverseSync watches Tunnel and updates the parent Service's
// Status.LoadBalancer.Ingress when AssignedIP changes.
type ReverseSync struct{ Client client.Client }

func (r *ReverseSync) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var t v1alpha1.Tunnel
    if err := r.Client.Get(ctx, req.NamespacedName, &t); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    if t.Status.AssignedIP == "" { return ctrl.Result{}, nil }

    var svc corev1.Service
    if err := r.Client.Get(ctx, types.NamespacedName{Namespace: t.Namespace, Name: t.Name}, &svc); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    if svc.Spec.LoadBalancerClass == nil || *svc.Spec.LoadBalancerClass != LoadBalancerClass { return ctrl.Result{}, nil }

    desired := []corev1.LoadBalancerIngress{{IP: t.Status.AssignedIP}}
    if loadBalancerEqual(svc.Status.LoadBalancer.Ingress, desired) { return ctrl.Result{}, nil }

    patch := client.MergeFrom(svc.DeepCopy())
    svc.Status.LoadBalancer.Ingress = desired
    return ctrl.Result{}, r.Client.Status().Patch(ctx, &svc, patch)
}

func (r *ReverseSync) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).Named("servicewatcher-reverse-sync").For(&v1alpha1.Tunnel{}).Complete(r)
}

func loadBalancerEqual(a, b []corev1.LoadBalancerIngress) bool { /* DeepEqual */ }
```

Tests: simulate Tunnel.Status.AssignedIP=10.0.0.5 → Service.Status.LoadBalancer.Ingress[0].IP=10.0.0.5.

Commit: `feat(servicewatcher): reverse-sync Tunnel.Status.AssignedIP → Service.Status`.

---

## Phase 8 acceptance checklist

- [x] `pkg/controllers/servicewatcher/` translates Service ↔ Tunnel.
- [x] Annotation contract per spec §3.4 (resources, requirements JSON, exit-pool shorthand, exit-claim-ref hard pin, do-not-disrupt).
- [x] Reverse sync populates Service.Status.LoadBalancer.Ingress.
- [x] envtest suite covers Create / Update / Delete / reverse-sync paths.
- [x] No CRD changes (Tunnel/Service shape unchanged).

## Out of scope

- TunnelTemplate CRD — deferred to v1beta1.
- Service finalizer — Tunnel cascade-delete via direct controller logic; no finalizer needed.
