package scheduling

// Results is what Solve returns.
type Results struct {
	// NewClaims are speculative ExitClaim CRs the provisioner should
	// create. Each has its name + spec ready to send to the apiserver.
	NewClaims []*InflightClaim
	// Bindings record (tunnel, exit-claim-name, resolved-ports) for
	// tunnels successfully placed onto an existing or inflight claim.
	Bindings []Binding
	// TunnelErrors tracks tunnels that couldn't be scheduled with the
	// reason. Provisioner surfaces these as Tunnel.Status conditions.
	TunnelErrors map[string]error
}

// Binding records a single tunnel→claim assignment with resolved ports.
type Binding struct {
	TunnelKey     string
	ExitClaimName string
	AssignedPorts []int32
}

// AllScheduled reports whether every input tunnel has a binding recorded.
func (r *Results) AllScheduled(inputTunnels int) bool {
	return len(r.Bindings) == inputTunnels
}
