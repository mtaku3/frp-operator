// Package provisioning hosts the singleton Provisioner that translates
// pending Tunnels into ExitClaim CRs. The Provisioner is fed by a
// trigger-batcher driven by the Tunnel and ExitClaim controllers in
// this package, and it executes a three-stage scheduling pipeline
// implemented in the scheduling subpackage.
package provisioning
