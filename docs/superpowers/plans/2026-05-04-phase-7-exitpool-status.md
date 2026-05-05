# Phase 7: ExitPool Ancillary Controllers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Drive `ExitPool.Status` surfaces — `Conditions[Ready, ProviderClassReady, ValidationSucceeded]`, `Status.Exits`, `Status.Resources` — and stamp pool template hashes so Phase 6's drift detection works. Four small write-only-back-to-the-CR controllers, each one responsibility.

**Architecture:** Four controllers each watching `ExitPool` (with cross-watches for ExitClaim where needed):
- **counter** — list ExitClaims labeled with this pool, sum their `Status.Allocatable` into `Pool.Status.Resources`, set `Status.Exits = len(claims)`. Updates feed `Phase 4 poolLimitsExceeded` (currently a TODO).
- **hash** — compute SHA-256 of `Pool.Spec.Template.Spec` (canonical JSON), stamp on `Pool.Annotations[frp.operator.io/pool-hash]`, AND on every child ExitClaim's same annotation. `Phase 6 drift method` compares the two annotations.
- **readiness** — resolve `Pool.Spec.Template.Spec.ProviderClassRef` to a real ProviderClass; set `Conditions[ProviderClassReady]` based on its existence + Ready condition.
- **validation** — CEL on the CRD covers most checks; this controller surfaces stateful checks (e.g. weight + replicas mutual exclusion already CEL-enforced; here we surface `Conditions[ValidationSucceeded]`).

**Spec:** §3.1 (ExitPool fields), §10 (no admission webhooks — status validation lives here).

**Prerequisites:** Phases 1-4 (Phases 5-6 land before or after; this phase doesn't depend on either, just on the API + state.Cluster).

**End state:**
- `pkg/controllers/exitpool/{counter,hash,readiness,validation}/` each have a controller.
- Each has its own envtest suite.
- Pool.Status surfaces become populated within a few reconciles.
- Pool template hash stamped on Pool.Annotations + child ExitClaim.Annotations.
- `make test` passes.

---

## File map

```
pkg/controllers/exitpool/
├── doc.go
├── counter/
│   ├── controller.go                    # rolls up child claims into Pool.Status.Resources / Exits
│   ├── controller_test.go               # envtest: create pool + 3 claims; status updates
│   └── suite_test.go
├── hash/
│   ├── controller.go                    # computes Pool.Spec.Template hash, stamps on Pool + children
│   ├── hash.go                          # canonical JSON marshalling + SHA-256
│   ├── hash_test.go                     # determinism: same spec → same hash; different spec → different
│   ├── controller_test.go
│   └── suite_test.go
├── readiness/
│   ├── controller.go                    # ProviderClassRef resolution → Conditions[ProviderClassReady]
│   └── controller_test.go
└── validation/
    ├── controller.go                    # surfaces stateful validation as Conditions[ValidationSucceeded]
    └── controller_test.go
```

---

## Task 1: hash/

**Files:** `pkg/controllers/exitpool/hash/{controller.go, hash.go, hash_test.go, controller_test.go, suite_test.go}`.

### hash.go (pure function)

```go
package hash

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PoolTemplateHash computes a canonical SHA-256 over the pool's
// Spec.Template.Spec. Same template → same hash regardless of map
// iteration order (json.Marshal sorts keys for json.Marshaler-less
// structs, which our types are). Used by Phase-6 drift detection.
func PoolTemplateHash(pool *v1alpha1.ExitPool) (string, error) {
    raw, err := json.Marshal(pool.Spec.Template.Spec)
    if err != nil { return "", err }
    sum := sha256.Sum256(raw)
    return hex.EncodeToString(sum[:])[:16], nil // 16 hex chars (64-bit) is plenty
}
```

Tests in `hash_test.go`:
1. Same spec → same hash on two calls.
2. Different `Frps.Version` → different hash.
3. Different `Requirements` order → SAME hash (canonical) — tricky if `[]NodeSelectorRequirementWithMinValues` is order-dependent; subagent: sort by Key before marshalling, OR document that order matters and canonicalize callers.

### controller.go

```go
package hash

import (
    "context"

    apierrors "k8s.io/apimachinery/pkg/api/errors"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

type Controller struct{ Client client.Client }

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var pool v1alpha1.ExitPool
    if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    h, err := PoolTemplateHash(&pool)
    if err != nil { return ctrl.Result{}, err }

    // 1. Stamp on Pool.Annotations (idempotent).
    if pool.Annotations[v1alpha1.AnnotationPoolHash] != h {
        if pool.Annotations == nil { pool.Annotations = map[string]string{} }
        pool.Annotations[v1alpha1.AnnotationPoolHash] = h
        if err := r.Client.Update(ctx, &pool); err != nil {
            return ctrl.Result{}, err
        }
    }

    // 2. Stamp on every child ExitClaim that lacks the current hash.
    var claims v1alpha1.ExitClaimList
    if err := r.Client.List(ctx, &claims, client.MatchingLabels{v1alpha1.LabelExitPool: pool.Name}); err != nil {
        return ctrl.Result{}, err
    }
    for i := range claims.Items {
        c := &claims.Items[i]
        if c.Annotations[v1alpha1.AnnotationPoolHash] == h { continue }
        if c.Annotations == nil { c.Annotations = map[string]string{} }
        c.Annotations[v1alpha1.AnnotationPoolHash] = h
        if err := r.Client.Update(ctx, c); err != nil {
            if apierrors.IsConflict(err) { continue }
            return ctrl.Result{}, err
        }
    }
    return ctrl.Result{}, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).Named("exitpool-hash").For(&v1alpha1.ExitPool{}).Complete(r)
}
```

Tests:
1. Pool gets annotation stamped on first reconcile.
2. Two child claims get same annotation.
3. Pool.Spec.Template.Frps.Version change updates Pool annotation; children get the NEW hash on next reconcile (drift detection input).

Commit: `feat(exitpool/hash): stamp template hash on Pool + child ExitClaims`.

---

## Task 2: counter/

```go
package counter

type Controller struct{ Client client.Client }

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var pool v1alpha1.ExitPool
    if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    var claims v1alpha1.ExitClaimList
    if err := r.Client.List(ctx, &claims, client.MatchingLabels{v1alpha1.LabelExitPool: pool.Name}); err != nil {
        return ctrl.Result{}, err
    }

    resources := corev1.ResourceList{}
    var exits int64
    for _, c := range claims.Items {
        if c.DeletionTimestamp != nil { continue }
        exits++
        for k, v := range c.Status.Allocatable {
            if cur, ok := resources[k]; ok { cur.Add(v); resources[k] = cur } else { resources[k] = v.DeepCopy() }
        }
    }
    // Add the count dimension.
    resources[corev1.ResourceName(v1alpha1.ResourceExits)] = *resourceQuantity(exits)

    if equalResourceLists(pool.Status.Resources, resources) && pool.Status.Exits == exits {
        return ctrl.Result{}, nil
    }
    patch := client.MergeFrom(pool.DeepCopy())
    pool.Status.Resources = resources
    pool.Status.Exits = exits
    return ctrl.Result{}, r.Client.Status().Patch(ctx, &pool, patch)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        Named("exitpool-counter").
        For(&v1alpha1.ExitPool{}).
        Watches(&v1alpha1.ExitClaim{}, &handler.EnqueueRequestsFromMapFunc(claimToPool)).
        Complete(r)
}

// claimToPool maps an ExitClaim to its owning ExitPool via the label.
func claimToPool(_ context.Context, obj client.Object) []ctrl.Request {
    poolName := obj.GetLabels()[v1alpha1.LabelExitPool]
    if poolName == "" { return nil }
    return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: poolName}}}
}
```

Tests: create pool + 3 claims → Status.Exits=3, Status.Resources sums their Allocatable. Delete a claim → Status.Exits=2.

Commit: `feat(exitpool/counter): roll up child claims into Pool.Status.{Exits,Resources}`.

---

## Task 3: readiness/

Resolves `Pool.Spec.Template.Spec.ProviderClassRef` to a typed object. If the kind is not in the scheme or the object doesn't exist → `Conditions[ProviderClassReady]=False`. Otherwise → True.

```go
type Controller struct {
    Client client.Client
    // KindToObject maps a ProviderClassRef.Kind ("LocalDockerProviderClass")
    // to a concrete object factory. Populated at operator startup
    // (Phase 9) from each registered provider's GetSupportedProviderClasses.
    KindToObject map[string]func() client.Object
}

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var pool v1alpha1.ExitPool
    if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    ref := pool.Spec.Template.Spec.ProviderClassRef
    factory, ok := r.KindToObject[ref.Kind]
    if !ok {
        setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse, v1alpha1.ReasonProviderClassNotFound, "kind not registered")
        return ctrl.Result{}, r.Client.Status().Update(ctx, &pool)
    }
    obj := factory()
    if err := r.Client.Get(ctx, types.NamespacedName{Name: ref.Name}, obj); err != nil {
        setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse, v1alpha1.ReasonProviderClassNotFound, err.Error())
        return ctrl.Result{}, r.Client.Status().Update(ctx, &pool)
    }
    setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "")
    setCond(&pool, v1alpha1.ConditionTypeReady, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "")
    return ctrl.Result{}, r.Client.Status().Update(ctx, &pool)
}
```

Tests: pool referencing nonexistent class → False; pool referencing existing class → True; pool's `Conditions[Ready]` follows.

Commit: `feat(exitpool/readiness): resolve ProviderClassRef into Pool.Conditions`.

---

## Task 4: validation/

Most validation lives in CEL on the CRD. This controller surfaces *stateful* checks. v1: just verify `weight` and `replicas` aren't both set (CEL covers it but as a defense in depth) and emit `Conditions[ValidationSucceeded]`. Easy to extend in v1beta1.

```go
type Controller struct{ Client client.Client }
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var pool v1alpha1.ExitPool
    if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil { return ctrl.Result{}, client.IgnoreNotFound(err) }
    if pool.Spec.Replicas != nil && pool.Spec.Weight != nil {
        setCond(&pool, v1alpha1.ConditionTypeValidationSucceeded, metav1.ConditionFalse, v1alpha1.ReasonInvalidRequirements, "spec.replicas and spec.weight are mutually exclusive")
        return ctrl.Result{}, r.Client.Status().Update(ctx, &pool)
    }
    setCond(&pool, v1alpha1.ConditionTypeValidationSucceeded, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "")
    return ctrl.Result{}, r.Client.Status().Update(ctx, &pool)
}
```

Tests: spec with both fields → False; spec with only weight → True.

Commit: `feat(exitpool/validation): surface stateful validation as Pool.Conditions`.

---

## Phase 7 acceptance checklist

- [x] Hash, counter, readiness, validation each in own sub-package, each one responsibility.
- [x] Phase 4 `poolLimitsExceeded` TODO closed: counter feeds the read.
- [x] Phase 6 drift method has the annotation it needs.
- [x] All envtest suites pass.
- [x] `go build`, `go vet` clean.

## Out of scope

- Real CEL refinement on CRDs — Phase 9.
- Cron-window-aware budget computation — already in Phase 6.
- Pool's `Spec.Replicas` static-mode behavior — feature-gated, not implemented.
