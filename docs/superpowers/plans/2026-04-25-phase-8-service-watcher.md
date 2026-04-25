# Phase 8: ServiceWatcherController Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** Bridge Kubernetes `Service type=LoadBalancer` resources with `spec.loadBalancerClass: frp-operator.io/frp` to operator-managed `Tunnel` CRs. The user creates a normal LoadBalancer Service (with the right class), and the operator transparently provisions a Tunnel + frpc + ExitServer underneath.

The controller has two responsibilities:

1. **Forward sync (Service → Tunnel):** Watch matching Services. For each, create/update a sibling Tunnel CR named after the Service. Translate Service.spec.ports + annotations into Tunnel.spec.

2. **Reverse sync (Tunnel.status → Service.status):** Once the Tunnel reaches `Ready` (or `Connecting` — see ingress-status policy below), write the assigned IP into `Service.status.loadBalancer.ingress[]` so `kubectl get svc` shows it as the user expects.

Annotation handling per spec §6.2:

| Service annotation | Tunnel.spec field |
|---|---|
| `frp-operator.io/exit: <name>` | `exitRef.name` |
| `frp-operator.io/provider: digitalocean` | appended to `placement.providers` |
| `frp-operator.io/region: nyc1,sfo3` | `placement.regions` |
| `frp-operator.io/size: s-2vcpu-2gb` | `placement.sizeOverride` |
| `frp-operator.io/scheduling-policy: <name>` | `schedulingPolicyRef.name` |
| `frp-operator.io/allow-port-split: "true"` | `allowPortSplit` |
| `frp-operator.io/migration-policy: OnExitLost` | `migrationPolicy` |
| `frp-operator.io/traffic-gb: "100"` | `requirements.monthlyTrafficGB` |
| `frp-operator.io/bandwidth-mbps: "200"` | `requirements.bandwidthMbps` |
| `frp-operator.io/immutable-when-ready: "true"` | `immutableWhenReady` |

**Design choices:**

- **Tunnel name = Service name.** Same namespace. Cleanup cascades when Service is deleted (we use a finalizer on the Service).
- **The class match.** `service.spec.loadBalancerClass == "frp-operator.io/frp"`. Other Services (no class, different class) are ignored. Standard Kubernetes idiom since 1.24.
- **Ingress status policy.** Write `Service.status.loadBalancer.ingress` when `Tunnel.Status.Phase ∈ {Connecting, Ready}` AND `Tunnel.Status.AssignedIP != ""`. Connecting-but-IP-known is good enough: the IP is allocated; the frpc Pod is starting. Production-correct would wait until Ready, but envtest-without-kubelet never reaches Ready, and operationally the IP being correct is what matters for kubectl UX.
- **Continuous sync.** Annotation changes propagate. `exitRef` rebinds (per spec §6.2 semantics — explicit user directive). `placement.*` is future-only. `migrationPolicy` future-only. `requirements.*` re-evaluates reservation math on the controller side (not in this phase).

**Architecture:**

```
internal/controller/servicewatcher_controller.go      — reconciler
internal/controller/servicewatcher_translator.go      — Service → Tunnel mapping (pure function)
internal/controller/servicewatcher_translator_test.go — 8+ table-driven cases
internal/controller/servicewatcher_controller_test.go — Ginkgo integration specs
config/rbac/role.yaml                                  — regenerated
```

**Tech Stack:** controller-runtime, envtest, Ginkgo. Imports `api/v1alpha1`, plus `corev1` for Service.

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §6.2 (Service annotation mapping), §3.2 (Tunnel CR).

**Out of scope:** TunnelController changes (Phase 6 already complete). Multi-port-split semantics (allowPortSplit propagates but the actual allocation behavior is Phase 6's). Service finalizer behavior under `--force-delete` (handled by the Service controller; we just observe deletion).

---

## File Structure

```
internal/controller/servicewatcher_translator.go      — pure Service→Tunnel translation
internal/controller/servicewatcher_translator_test.go
internal/controller/servicewatcher_controller.go      — reconciler
internal/controller/servicewatcher_controller_test.go
```

**Boundaries.** The translator is pure: takes a `*corev1.Service`, returns a `*frpv1alpha1.Tunnel`. No I/O. Reconciler does I/O (Get, Create/Update Tunnel, Patch Service.status).

---

## Task 1: Service → Tunnel translator

**Files:**
- Create: `internal/controller/servicewatcher_translator.go`
- Test: `internal/controller/servicewatcher_translator_test.go`

A pure function `translateServiceToTunnelSpec(*corev1.Service) (frpv1alpha1.TunnelSpec, error)`. Errors only on syntactically invalid annotations (e.g. `frp-operator.io/traffic-gb: "abc"`). Empty annotations → empty/zero fields.

- [ ] **Step 1: Write the test (table-driven, ~10 cases)**

`internal/controller/servicewatcher_translator_test.go`:

```go
package controller

import (
    "reflect"
    "testing"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/intstr"
    "k8s.io/utils/ptr"

    frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func mkSvc(opts ...func(*corev1.Service)) *corev1.Service {
    s := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns"},
        Spec: corev1.ServiceSpec{
            Type:              corev1.ServiceTypeLoadBalancer,
            LoadBalancerClass: ptr.To("frp-operator.io/frp"),
            Ports: []corev1.ServicePort{
                {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
            },
        },
    }
    for _, o := range opts {
        o(s)
    }
    return s
}

func TestTranslateServiceToTunnelSpec(t *testing.T) {
    cases := []struct {
        name    string
        svc     *corev1.Service
        want    frpv1alpha1.TunnelSpec
        wantErr bool
    }{
        {
            name: "minimal LoadBalancer with one port",
            svc:  mkSvc(),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "exit annotation hard-pins exitRef",
            svc: mkSvc(func(s *corev1.Service) {
                s.Annotations = map[string]string{"frp-operator.io/exit": "exit-nyc-1"}
            }),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                ExitRef: &frpv1alpha1.ExitRef{Name: "exit-nyc-1"},
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "provider/region/size annotations populate placement",
            svc: mkSvc(func(s *corev1.Service) {
                s.Annotations = map[string]string{
                    "frp-operator.io/provider": "digitalocean",
                    "frp-operator.io/region":   "nyc1,sfo3",
                    "frp-operator.io/size":     "s-2vcpu-2gb",
                }
            }),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                Placement: &frpv1alpha1.Placement{
                    Providers:    []frpv1alpha1.Provider{frpv1alpha1.ProviderDigitalOcean},
                    Regions:      []string{"nyc1", "sfo3"},
                    SizeOverride: "s-2vcpu-2gb",
                },
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "scheduling-policy + migration-policy + allow-port-split",
            svc: mkSvc(func(s *corev1.Service) {
                s.Annotations = map[string]string{
                    "frp-operator.io/scheduling-policy":  "team-a",
                    "frp-operator.io/migration-policy":   "OnExitLost",
                    "frp-operator.io/allow-port-split":   "true",
                    "frp-operator.io/immutable-when-ready": "true",
                }
            }),
            want: frpv1alpha1.TunnelSpec{
                Service:             frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "team-a"},
                MigrationPolicy:     frpv1alpha1.MigrationOnExitLost,
                AllowPortSplit:      true,
                ImmutableWhenReady:  true,
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "traffic-gb and bandwidth-mbps annotations populate requirements",
            svc: mkSvc(func(s *corev1.Service) {
                s.Annotations = map[string]string{
                    "frp-operator.io/traffic-gb":     "100",
                    "frp-operator.io/bandwidth-mbps": "200",
                }
            }),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                Requirements: &frpv1alpha1.TunnelRequirements{
                    MonthlyTrafficGB: ptr.To(int64(100)),
                    BandwidthMbps:    ptr.To(int32(200)),
                },
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "non-numeric traffic-gb errors",
            svc: mkSvc(func(s *corev1.Service) {
                s.Annotations = map[string]string{"frp-operator.io/traffic-gb": "abc"}
            }),
            wantErr: true,
        },
        {
            name: "multi-port Service",
            svc: mkSvc(func(s *corev1.Service) {
                s.Spec.Ports = []corev1.ServicePort{
                    {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
                    {Name: "https", Port: 443, TargetPort: intstr.FromInt(8443), Protocol: corev1.ProtocolTCP},
                }
            }),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
                    {Name: "https", ServicePort: 443, PublicPort: ptr.To(int32(443)), Protocol: frpv1alpha1.ProtocolTCP},
                },
            },
        },
        {
            name: "UDP port",
            svc: mkSvc(func(s *corev1.Service) {
                s.Spec.Ports = []corev1.ServicePort{
                    {Name: "dns", Port: 53, TargetPort: intstr.FromInt(53), Protocol: corev1.ProtocolUDP},
                }
            }),
            want: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
                Ports: []frpv1alpha1.TunnelPort{
                    {Name: "dns", ServicePort: 53, PublicPort: ptr.To(int32(53)), Protocol: frpv1alpha1.ProtocolUDP},
                },
            },
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got, err := translateServiceToTunnelSpec(tc.svc)
            if (err != nil) != tc.wantErr {
                t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
            }
            if tc.wantErr {
                return
            }
            if !reflect.DeepEqual(got, tc.want) {
                t.Errorf("translation mismatch\ngot:  %+v\nwant: %+v", got, tc.want)
            }
        })
    }
}

func TestServiceMatchesClass(t *testing.T) {
    cases := []struct {
        name string
        svc  *corev1.Service
        want bool
    }{
        {"matching class", mkSvc(), true},
        {"different class", mkSvc(func(s *corev1.Service) { s.Spec.LoadBalancerClass = ptr.To("other") }), false},
        {"no class set", mkSvc(func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil }), false},
        {"not a LoadBalancer Service", mkSvc(func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeClusterIP }), false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if serviceMatchesClass(tc.svc) != tc.want {
                t.Errorf("got %v want %v", !tc.want, tc.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/controller/ -run 'Translate|ServiceMatches' -v`

- [ ] **Step 3: Implement**

`internal/controller/servicewatcher_translator.go`:

```go
package controller

import (
    "fmt"
    "strconv"
    "strings"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/utils/ptr"

    frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// loadBalancerClassName is the class string the operator matches.
const loadBalancerClassName = "frp-operator.io/frp"

// Annotation keys for Service → Tunnel translation. See spec §6.2.
const (
    annExit              = "frp-operator.io/exit"
    annProvider          = "frp-operator.io/provider"
    annRegion            = "frp-operator.io/region"
    annSize              = "frp-operator.io/size"
    annSchedulingPolicy  = "frp-operator.io/scheduling-policy"
    annAllowPortSplit    = "frp-operator.io/allow-port-split"
    annMigrationPolicy   = "frp-operator.io/migration-policy"
    annTrafficGB         = "frp-operator.io/traffic-gb"
    annBandwidthMbps     = "frp-operator.io/bandwidth-mbps"
    annImmutableWhenReady = "frp-operator.io/immutable-when-ready"
)

// serviceMatchesClass reports whether the Service is one this operator
// owns: type=LoadBalancer with the right loadBalancerClass.
func serviceMatchesClass(svc *corev1.Service) bool {
    if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
        return false
    }
    if svc.Spec.LoadBalancerClass == nil {
        return false
    }
    return *svc.Spec.LoadBalancerClass == loadBalancerClassName
}

// translateServiceToTunnelSpec is a pure function: Service in, TunnelSpec
// out. Returns an error only on syntactically invalid annotations.
func translateServiceToTunnelSpec(svc *corev1.Service) (frpv1alpha1.TunnelSpec, error) {
    spec := frpv1alpha1.TunnelSpec{
        Service: frpv1alpha1.ServiceRef{Name: svc.Name, Namespace: svc.Namespace},
    }
    for _, p := range svc.Spec.Ports {
        proto := frpv1alpha1.ProtocolTCP
        if p.Protocol == corev1.ProtocolUDP {
            proto = frpv1alpha1.ProtocolUDP
        }
        spec.Ports = append(spec.Ports, frpv1alpha1.TunnelPort{
            Name:        p.Name,
            ServicePort: p.Port,
            PublicPort:  ptr.To(p.Port), // public == service port unless overridden in future
            Protocol:    proto,
        })
    }

    a := svc.Annotations
    if v := a[annExit]; v != "" {
        spec.ExitRef = &frpv1alpha1.ExitRef{Name: v}
    }
    placement := &frpv1alpha1.Placement{}
    placementSet := false
    if v := a[annProvider]; v != "" {
        placement.Providers = append(placement.Providers, frpv1alpha1.Provider(v))
        placementSet = true
    }
    if v := a[annRegion]; v != "" {
        for _, r := range strings.Split(v, ",") {
            r = strings.TrimSpace(r)
            if r != "" {
                placement.Regions = append(placement.Regions, r)
            }
        }
        placementSet = true
    }
    if v := a[annSize]; v != "" {
        placement.SizeOverride = v
        placementSet = true
    }
    if placementSet {
        spec.Placement = placement
    }
    if v := a[annSchedulingPolicy]; v != "" {
        spec.SchedulingPolicyRef = frpv1alpha1.PolicyRef{Name: v}
    }
    if v := a[annAllowPortSplit]; v == "true" {
        spec.AllowPortSplit = true
    }
    if v := a[annMigrationPolicy]; v != "" {
        spec.MigrationPolicy = frpv1alpha1.MigrationPolicy(v)
    }
    if v := a[annImmutableWhenReady]; v == "true" {
        spec.ImmutableWhenReady = true
    }

    var reqSet bool
    req := &frpv1alpha1.TunnelRequirements{}
    if v := a[annTrafficGB]; v != "" {
        n, err := strconv.ParseInt(v, 10, 64)
        if err != nil {
            return frpv1alpha1.TunnelSpec{}, fmt.Errorf("annotation %s=%q: %w", annTrafficGB, v, err)
        }
        req.MonthlyTrafficGB = ptr.To(n)
        reqSet = true
    }
    if v := a[annBandwidthMbps]; v != "" {
        n, err := strconv.ParseInt(v, 10, 32)
        if err != nil {
            return frpv1alpha1.TunnelSpec{}, fmt.Errorf("annotation %s=%q: %w", annBandwidthMbps, v, err)
        }
        req.BandwidthMbps = ptr.To(int32(n))
        reqSet = true
    }
    if reqSet {
        spec.Requirements = req
    }

    return spec, nil
}
```

- [ ] **Step 4: Run, confirm PASS**

`devbox run -- go test ./internal/controller/ -run 'Translate|ServiceMatches' -v`
All 8+ translator sub-tests + 4 class-match sub-tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/servicewatcher_translator.go internal/controller/servicewatcher_translator_test.go
git commit -m "feat(controller/servicewatcher): pure Service to Tunnel translator with annotation mapping"
```

---

## Task 2: ServiceWatcherController

**Files:**
- Create: `internal/controller/servicewatcher_controller.go`
- Test: `internal/controller/servicewatcher_controller_test.go`

The reconciler. Watches `corev1.Service`. For each:
1. If not matching the class → no-op (return).
2. If marked for deletion → delete the sibling Tunnel, return.
3. Translate to TunnelSpec.
4. Look for an existing Tunnel with the same name. Create if absent, Update if drifted.
5. If Tunnel.Status.AssignedIP != "" and Phase is Connecting/Ready, patch Service.status.loadBalancer.ingress.

The Tunnel is owned by the Service via OwnerReference, so Service deletion cascades.

- [ ] **Step 1: Write the controller**

```go
package controller

import (
    "context"
    "fmt"
    "reflect"

    corev1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/utils/ptr"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"

    frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ServiceWatcherReconciler watches LoadBalancer Services with our class and
// drives a sibling Tunnel CR. Reverse-syncs Tunnel.Status.AssignedIP into
// Service.status.loadBalancer.ingress so kubectl shows it as an external IP.
type ServiceWatcherReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one Service.
func (r *ServiceWatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    var svc corev1.Service
    if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }
    if !serviceMatchesClass(&svc) {
        return ctrl.Result{}, nil
    }
    if !svc.DeletionTimestamp.IsZero() {
        // Service is being deleted; the owner-ref ensures the Tunnel
        // gets garbage-collected. Nothing to do here.
        return ctrl.Result{}, nil
    }

    desiredSpec, err := translateServiceToTunnelSpec(&svc)
    if err != nil {
        logger.Error(err, "translate Service to TunnelSpec")
        return ctrl.Result{}, err
    }

    // Reconcile the sibling Tunnel.
    var tunnel frpv1alpha1.Tunnel
    err = r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &tunnel)
    if apierrors.IsNotFound(err) {
        tunnel = frpv1alpha1.Tunnel{
            ObjectMeta: metav1.ObjectMeta{
                Name:      svc.Name,
                Namespace: svc.Namespace,
                Labels:    map[string]string{"frp-operator.io/created-by": "service-watcher"},
                OwnerReferences: []metav1.OwnerReference{{
                    APIVersion:         "v1",
                    Kind:               "Service",
                    Name:               svc.Name,
                    UID:                svc.UID,
                    BlockOwnerDeletion: ptr.To(true),
                    Controller:         ptr.To(true),
                }},
            },
            Spec: desiredSpec,
        }
        if err := r.Create(ctx, &tunnel); err != nil {
            return ctrl.Result{}, fmt.Errorf("create Tunnel: %w", err)
        }
        return ctrl.Result{Requeue: true}, nil
    }
    if err != nil {
        return ctrl.Result{}, fmt.Errorf("get Tunnel: %w", err)
    }

    // Drift: update spec if changed.
    if !reflect.DeepEqual(tunnel.Spec, desiredSpec) {
        tunnel.Spec = desiredSpec
        if err := r.Update(ctx, &tunnel); err != nil {
            return ctrl.Result{}, fmt.Errorf("update Tunnel: %w", err)
        }
    }

    // Reverse-sync: write the assigned IP into Service.status if the tunnel
    // has one and is at least Connecting.
    if tunnel.Status.AssignedIP != "" &&
        (tunnel.Status.Phase == frpv1alpha1.TunnelConnecting ||
            tunnel.Status.Phase == frpv1alpha1.TunnelReady) {
        return r.syncIngressStatus(ctx, &svc, tunnel.Status.AssignedIP)
    }
    return ctrl.Result{}, nil
}

// syncIngressStatus writes ip into svc.status.loadBalancer.ingress[] if it
// isn't already there.
func (r *ServiceWatcherReconciler) syncIngressStatus(ctx context.Context, svc *corev1.Service, ip string) (ctrl.Result, error) {
    for _, in := range svc.Status.LoadBalancer.Ingress {
        if in.IP == ip {
            return ctrl.Result{}, nil
        }
    }
    patch := client.MergeFrom(svc.DeepCopy())
    svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: ip}}
    if err := r.Status().Patch(ctx, svc, patch); err != nil {
        return ctrl.Result{}, fmt.Errorf("patch service status: %w", err)
    }
    return ctrl.Result{}, nil
}

// SetupWithManager wires the controller. Watches Services and Tunnels (so
// Tunnel status changes trigger Service status reverse-sync).
func (r *ServiceWatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&corev1.Service{}).
        Owns(&frpv1alpha1.Tunnel{}).
        Named("servicewatcher").
        Complete(r)
}
```

- [ ] **Step 2: Write integration tests**

```go
package controller

import (
    "context"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    corev1 "k8s.io/api/core/v1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/apimachinery/pkg/util/intstr"
    "k8s.io/utils/ptr"
    ctrl "sigs.k8s.io/controller-runtime"

    frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

var _ = Describe("ServiceWatcherController", func() {
    ctx := context.Background()
    var recon *ServiceWatcherReconciler

    BeforeEach(func() {
        recon = &ServiceWatcherReconciler{Client: k8sClient, Scheme: scheme.Scheme}
    })

    It("creates a Tunnel for a matching Service", func() {
        svc := &corev1.Service{
            ObjectMeta: metav1.ObjectMeta{Name: "sw-1", Namespace: "default"},
            Spec: corev1.ServiceSpec{
                Type:              corev1.ServiceTypeLoadBalancer,
                LoadBalancerClass: ptr.To("frp-operator.io/frp"),
                Ports: []corev1.ServicePort{
                    {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
                },
            },
        }
        Expect(k8sClient.Create(ctx, svc)).To(Succeed())
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

        req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-1", Namespace: "default"}}
        _, err := recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        var t frpv1alpha1.Tunnel
        Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
        Expect(t.Spec.Service.Name).To(Equal("sw-1"))
        Expect(t.Spec.Ports).To(HaveLen(1))
        Expect(t.Spec.Ports[0].ServicePort).To(Equal(int32(80)))
        // Tunnel is owned by Service.
        Expect(t.OwnerReferences).To(HaveLen(1))
        Expect(t.OwnerReferences[0].Kind).To(Equal("Service"))
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })
    })

    It("ignores Services without the matching class", func() {
        svc := &corev1.Service{
            ObjectMeta: metav1.ObjectMeta{Name: "sw-other", Namespace: "default"},
            Spec: corev1.ServiceSpec{
                Type:              corev1.ServiceTypeLoadBalancer,
                LoadBalancerClass: ptr.To("other-vendor/lb"),
                Ports: []corev1.ServicePort{
                    {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
                },
            },
        }
        Expect(k8sClient.Create(ctx, svc)).To(Succeed())
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

        req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-other", Namespace: "default"}}
        _, err := recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        // No Tunnel created.
        var t frpv1alpha1.Tunnel
        err = k8sClient.Get(ctx, req.NamespacedName, &t)
        Expect(apierrors.IsNotFound(err)).To(BeTrue())
    })

    It("syncs assigned IP into Service.status.loadBalancer.ingress", func() {
        svc := &corev1.Service{
            ObjectMeta: metav1.ObjectMeta{Name: "sw-3", Namespace: "default"},
            Spec: corev1.ServiceSpec{
                Type:              corev1.ServiceTypeLoadBalancer,
                LoadBalancerClass: ptr.To("frp-operator.io/frp"),
                Ports: []corev1.ServicePort{
                    {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
                },
            },
        }
        Expect(k8sClient.Create(ctx, svc)).To(Succeed())
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

        req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-3", Namespace: "default"}}
        // First reconcile: create Tunnel.
        _, err := recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        // Simulate TunnelController having advanced status to Connecting+IP.
        var t frpv1alpha1.Tunnel
        Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })
        t.Status.Phase = frpv1alpha1.TunnelConnecting
        t.Status.AssignedIP = "203.0.113.42"
        t.Status.AssignedExit = "exit-x"
        t.Status.AssignedPorts = []int32{80}
        Expect(k8sClient.Status().Update(ctx, &t)).To(Succeed())

        // Second reconcile: should patch Service.status.
        _, err = recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        var got corev1.Service
        Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
        Expect(got.Status.LoadBalancer.Ingress).To(HaveLen(1))
        Expect(got.Status.LoadBalancer.Ingress[0].IP).To(Equal("203.0.113.42"))
    })

    It("updates Tunnel.spec when annotations change", func() {
        svc := &corev1.Service{
            ObjectMeta: metav1.ObjectMeta{
                Name: "sw-4", Namespace: "default",
                Annotations: map[string]string{"frp-operator.io/region": "nyc1"},
            },
            Spec: corev1.ServiceSpec{
                Type:              corev1.ServiceTypeLoadBalancer,
                LoadBalancerClass: ptr.To("frp-operator.io/frp"),
                Ports: []corev1.ServicePort{
                    {Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
                },
            },
        }
        Expect(k8sClient.Create(ctx, svc)).To(Succeed())
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

        req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sw-4", Namespace: "default"}}
        _, err := recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        var t frpv1alpha1.Tunnel
        Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
        Expect(t.Spec.Placement).NotTo(BeNil())
        Expect(t.Spec.Placement.Regions).To(Equal([]string{"nyc1"}))
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, &t) })

        // Update annotation.
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sw-4", Namespace: "default"}, svc)).To(Succeed())
        svc.Annotations["frp-operator.io/region"] = "sfo3"
        Expect(k8sClient.Update(ctx, svc)).To(Succeed())

        _, err = recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())

        Expect(k8sClient.Get(ctx, req.NamespacedName, &t)).To(Succeed())
        Expect(t.Spec.Placement.Regions).To(Equal([]string{"sfo3"}))
    })
})
```

- [ ] **Step 3: Run, confirm tests pass**

`devbox run -- make test`

Total controller specs should be 17 (13 from Phase 7 + 4 from this task).

- [ ] **Step 4: Commit**

```bash
git add internal/controller/servicewatcher_controller.go internal/controller/servicewatcher_controller_test.go
git commit -m "feat(controller/servicewatcher): Service-to-Tunnel reconciler with reverse-status sync"
```

---

## Phase 8 done — exit criteria

- `devbox run -- make test` green: all packages.
- `internal/controller/servicewatcher_controller.go` reconciles `Service type=LoadBalancer` with class `frp-operator.io/frp` into a sibling Tunnel CR (owned by the Service).
- Annotations on the Service propagate continuously to Tunnel.spec.
- Once Tunnel reaches Connecting/Ready and has an AssignedIP, the IP appears in `Service.status.loadBalancer.ingress[]`.
- Four integration specs: create-tunnel, ignore-other-class, status-reverse-sync, annotation-update.

The next plan (Phase 9: DigitalOcean provider + main.go wiring) builds the production cloud path and finally wires `cmd/manager/main.go` so the operator can run end-to-end against a real cluster.
