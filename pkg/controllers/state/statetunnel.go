package state

// TunnelBinding records the live assignment of a Tunnel to an ExitClaim
// (denormalized from Tunnel.Status). Cluster.Bindings is keyed by
// "<ns>/<name>"; the value points at an ExitClaim by name (cluster-scoped
// CR so namespace not relevant).
type TunnelBinding struct {
	TunnelKey     TunnelKey
	ExitClaimName string
	AssignedPorts []int32
}
