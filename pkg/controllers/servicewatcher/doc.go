// Package servicewatcher translates a corev1.Service of
// loadBalancerClass=frp.operator.io/frp into a sibling Tunnel CR (same
// name + namespace) and reverse-syncs Tunnel.Status.AssignedIP back into
// Service.Status.LoadBalancer.Ingress. Service annotations
// (frp.operator.io/*) carry tunnel-level config: resources, requirements
// (raw JSON or exit-pool shorthand), exit-claim hard-pin, and the
// do-not-disrupt opt-out. The Tunnel's lifecycle (scheduling,
// provisioning, disruption) is owned by the Phase 4-7 controllers; this
// package only maintains the translation.
package servicewatcher
