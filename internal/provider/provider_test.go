package provider

import "testing"

// TestProvisionerInterfaceShape is a compile-time sanity check that documents
// the contract: any type that satisfies the Provisioner interface MUST have
// these four methods. If this stops compiling, the interface changed and
// callers across the codebase need to be updated.
func TestProvisionerInterfaceShape(t *testing.T) {
	var _ Provisioner = (*stubProvisioner)(nil)
}
