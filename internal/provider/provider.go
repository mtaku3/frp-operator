// Package provider defines the Provisioner abstraction the operator uses to
// create and destroy backing infrastructure (cloud VPSes, Docker containers,
// or pre-existing external instances).
//
// Concrete implementations live in subpackages: provider/fake, provider/localdocker,
// provider/digitalocean (added in Phase 9). They register themselves with the
// Registry at process start so SchedulingPolicy can select one by name.
package provider

import (
	"context"
	"errors"
)

// Spec is the desired configuration of one exit's underlying VPS / container.
// Fields not relevant to a given Provisioner (e.g., Region for LocalDocker)
// are ignored; the Provisioner's documentation states what it requires.
type Spec struct {
	// Name is the operator-assigned identifier; provisioners use it as a
	// stable handle to look up state across reconcile loops. Typically
	// "<namespace>__<exitserver-name>".
	Name string

	// Region is provider-specific (e.g., "nyc1"). LocalDocker ignores it.
	Region string

	// Size is provider-specific SKU (e.g., "s-1vcpu-1gb"). LocalDocker ignores it.
	Size string

	// CloudInitUserData is the rendered cloud-init user-data script (from
	// internal/bootstrap.Render). DigitalOcean passes it as droplet
	// user-data. LocalDocker doesn't use it (the container starts frps
	// directly from a mounted config).
	CloudInitUserData []byte

	// FrpsConfigTOML is the rendered frps.toml content. Used by LocalDocker
	// (mounted as a volume). Cloud providers receive this via cloud-init.
	FrpsConfigTOML []byte

	// Credentials is provider-specific (e.g., DO API token). Opaque blob;
	// the Registry lookup decides how to interpret it.
	Credentials []byte

	// AdminPort, BindPort: ports the operator must be able to reach.
	// LocalDocker publishes them on the host; cloud providers open them
	// via cloud-init firewall rules.
	AdminPort int
	BindPort  int
}

// State is the observed condition of one provisioned resource. Returned by
// Create (after creation completes) and Inspect (on demand).
type State struct {
	// ProviderID is a stable identifier the Provisioner can use to look up
	// the resource in subsequent calls (DO droplet ID, Docker container ID).
	ProviderID string

	// PublicIP is the address the operator and external clients dial.
	// LocalDocker returns "127.0.0.1".
	PublicIP string

	// Phase reports the lifecycle phase the underlying resource is in.
	Phase Phase

	// Reason is a human-readable note for debugging when Phase is Failed
	// or Unreachable. Empty when state is healthy.
	Reason string
}

// Phase enumerates the lifecycle states a Provisioner can report. These map
// 1:1 to ExitServer.status.phase, but the Provisioner deals only with the
// underlying infrastructure — health probes and finalizer handling live in
// the controller.
type Phase string

const (
	PhaseProvisioning Phase = "Provisioning"
	PhaseRunning      Phase = "Running"
	PhaseFailed       Phase = "Failed"
	PhaseGone         Phase = "Gone" // Inspected and confirmed destroyed
)

// Provisioner is implemented by each backend (DigitalOcean, LocalDocker,
// External, Fake). All methods are context-cancellable.
type Provisioner interface {
	// Name returns the provisioner's stable identifier. Must match the
	// SchedulingPolicy.spec.provider value used to select it.
	Name() string

	// Create makes the underlying resource. It blocks until the resource
	// is at least in PhaseProvisioning (the controller polls Inspect
	// afterwards). Returns the initial State (typically with ProviderID
	// populated, PublicIP empty until ready).
	Create(ctx context.Context, spec Spec) (State, error)

	// Destroy tears down the resource identified by providerID. Idempotent
	// — destroying an already-gone resource returns nil error. Bubble
	// non-recoverable errors so the caller can surface them on the CR.
	Destroy(ctx context.Context, providerID string) error

	// Inspect returns the current State of providerID. If the resource is
	// genuinely gone (deleted out of band), return ErrNotFound — the
	// controller treats that as PhaseLost. Transient errors (network
	// blips) should be returned as-is so the controller can retry.
	Inspect(ctx context.Context, providerID string) (State, error)
}

// Sentinel errors. Wrap with fmt.Errorf("...: %w", err) when adding context.
var (
	// ErrNotFound is returned by Inspect when the resource is confirmed
	// gone (e.g., DO 404, Docker container removed).
	ErrNotFound = errors.New("provider: resource not found")

	// ErrNotRegistered is returned by Registry.Lookup when no Provisioner
	// is registered under the requested name.
	ErrNotRegistered = errors.New("provider: not registered")
)
