# Karpenter conformance — scope and divergences

This document records the deliberate scope decisions for frp-operator's
karpenter-shaped disruption / scheduling model. It closes audit issue
#13 and serves as the reference for future contributors evaluating
"is this karpenter-strict?".

The four-CRD shape, singleton provisioner with batcher, three-stage
Solve, drift / expiration / consolidation methods, cordon-via-condition,
emptiness condition, continuous liveness probe, and offering-priced
instance-type selection are all conformant. The remaining gaps versus
upstream karpenter are listed below with their resolutions.

## In scope (implemented)

| Concept | Karpenter | frp-operator |
|---|---|---|
| NodePool / NodeClaim / NodeClass / Pod | NodePool / NodeClaim / EC2NodeClass / Pod | ExitPool / ExitClaim / *ProviderClass / Tunnel |
| Discovery selectors on NodeClass | `subnetSelectorTerms`, `amiSelectorTerms` | `imageSelectorTerms` (DO) |
| Shape requirements on NodePool | `requirements: [instance-type, instance-family, capacity-type, ...]` | `requirements: [node.kubernetes.io/instance-type, topology.kubernetes.io/region, ...]` |
| Cheapest-offering selection | `pricing.Provider` + `Offering.Price` sort | `cloudprovider.Offering.Price` sort in `scheduling.SelectInstanceType` |
| Claim spec narrowing at provision time | NodeClaim Requirements pinned by scheduler | ExitClaim Requirements pinned via `scheduling.PinChosen` |
| Drift detection | `karpenter.sh/nodeclass-hash` + `karpenter.sh/nodepool-hash` | `frp.operator.io/providerclass-hash` + `frp.operator.io/pool-hash` |
| Empty condition | `Empty` LastTransitionTime | `Empty` stamped by `pkg/controllers/exitclaim/emptiness` |
| Cordon before replace | `karpenter.sh/disruption` taint | `Disrupted=True` condition stamped by disruption Queue |
| Continuous health probe | NodeHealth controller + `RepairPolicies` | post-Ready admin-API probe in `pkg/controllers/exitclaim/lifecycle/Liveness` with consecutive-failure threshold |
| Disruption methods | Emptiness, Drift, Expiration, MultiNode/SingleNode Consolidation | same set |
| Cheaper-replacement consolidation | `disruption/multinodeconsolidation` cheaper-shape branch | `Simulator.CanRepackWithReplacement` |
| Disruption budgets | per-pool/per-reason/per-cron | identical |
| Per-pool resource limits | `NodePool.Spec.Limits` | `ExitPool.Spec.Limits` rolled up by `pkg/controllers/exitpool/counter` |

## Out of scope (declared deferred or N/A)

These are listed so readers don't conclude the divergence is an oversight.

### C1 — Taints / Tolerations

**Status:** deferred to v1alpha2 or later.

Karpenter NodePool template carries `taints` and `startupTaints`; pods
opt in via `tolerations`. The frp domain has no analog today: every
Tunnel is a uniform Service-derived workload and no tenant requires
"this tunnel only on dedicated exits". Adding `Taints` to
`ExitClaimTemplateSpec` and `Tolerations` to `TunnelSpec` is a one-PR
change when a use case appears (multi-tenant gold/silver tiers, GPU
exits, etc.).

### C2 — Topology spread constraints

**Status:** out of scope. Multi-region spreading is achieved via
multi-pool — one `ExitPool` per region with non-overlapping
`topology.kubernetes.io/region` requirements. `topologySpreadConstraints`
on TunnelSpec would be redundant for the current single-zone DO/local
runtimes. Revisit if a provider with intra-region zone topology is
added.

### C3 — Pod affinity / anti-affinity

**Status:** out of scope. Tunnel-to-tunnel co-location rules have no
clear use case in the frp domain.

### C4 — DaemonSet overhead modeling

**Status:** N/A. Karpenter accounts for DaemonSet pods that follow
each new node. Frp-operator has no DaemonSet equivalent; per-claim
overhead is a static `Overhead` field on `cloudprovider.InstanceType`,
already subtracted in `Allocatable()`.

### C5 — Volume topology

**Status:** N/A. No PersistentVolumes.

### C6 — Spot vs on-demand capacity-type

**Status:** deferred. DigitalOcean has no spot pricing. The
`frp.operator.io/capacity-type` requirement constant is reserved for
the AWS provider when added; the scheduler already filters offerings
by requirement keys, so wiring up capacity-type-aware sort is a
provider-side change only.

### C7 — Karpenter naming convention

**Status:** deliberate rebrand. Karpenter uses `karpenter.sh/*` for
domain labels, annotations, finalizers. We use `frp.operator.io/*`
because we are a fork-of-shape, not a fork-of-karpenter binary. The
behavior matches; the prefix differs. This mirrors how every karpenter
provider rebranding (cluster-api, etc.) handles the namespace.

The standard k8s label keys (`node.kubernetes.io/instance-type`,
`topology.kubernetes.io/region`, `topology.kubernetes.io/zone`,
`kubernetes.io/arch`) are NOT rebranded — they're upstream conventions
the kube ecosystem expects.

## Acceptable but documented limitations

- **Pair-only multi-consolidation.** `MultiNodeConsolidation` operates
  on `(i, i+1)` pairs greedy. Karpenter's binary search over arbitrary
  subsets is a known gap; v1alpha2 candidate.

- **Pricing is static.** `cloudprovider.InstanceType.Offerings[].Price`
  is hardcoded per droplet size in `pkg/cloudprovider/digitalocean/
  instancetype.go`. Karpenter's AWS provider polls live spot prices.
  Fine for DO (no spot); revisit when a spot-capable provider is added.

## Closing the audit

Issue #13 is closed by this document. Remaining work for v1alpha2 / a
hypothetical v1 stabilization: arbitrary-subset multi-consolidation,
taints/tolerations, dynamic pricing if a spot provider is added.
