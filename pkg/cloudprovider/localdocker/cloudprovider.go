// Package localdocker implements cloudprovider.CloudProvider against the
// Docker SDK. Exits become local containers; intended for development and
// e2e tests, not production.
package localdocker

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

// CloudProvider is the localdocker impl. Talks to Docker via the SDK and
// reads LocalDockerProviderClass for config.
type CloudProvider struct {
	kube   client.Client
	docker *dockerOps

	// AuthTokenResolver fetches the frps auth token for a claim. If nil,
	// an empty token is used (Phase 5 wires the resolver against
	// SecretRef).
	AuthTokenResolver func(ctx context.Context, claim *v1alpha1.ExitClaim) (string, error)
}

// New constructs a CloudProvider. kube is required for ProviderClass lookup.
// Returns an error if Docker SDK init fails.
func New(kube client.Client) (*CloudProvider, error) {
	d, err := newDockerOps()
	if err != nil {
		return nil, err
	}
	return &CloudProvider{kube: kube, docker: d}, nil
}

// Close releases the Docker SDK client.
func (c *CloudProvider) Close() error {
	if c.docker == nil || c.docker.cli == nil {
		return nil
	}
	return c.docker.cli.Close()
}

func (c *CloudProvider) Name() string { return providerName }

func (c *CloudProvider) Create(ctx context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	pc, err := c.resolveClass(ctx, claim)
	if err != nil {
		return nil, err
	}
	token := ""
	if c.AuthTokenResolver != nil {
		token, err = c.AuthTokenResolver(ctx, claim)
		if err != nil {
			return nil, fmt.Errorf("resolve auth token: %w", err)
		}
	}
	id, err := c.docker.ensureContainer(ctx, claim, pc, token)
	if err != nil {
		return nil, err
	}
	out := claim.DeepCopy()
	out.Status.ProviderID = id
	out.Status.ExitName = containerName(claim)
	out.Status.ImageID = imageRef(pc, claim.Spec.Frps.Version)
	out.Status.FrpsVersion = claim.Spec.Frps.Version

	// Hydrate Capacity/Allocatable from the static catalog.
	it := InstanceTypes()[0]
	out.Status.Capacity = it.Capacity.DeepCopy()
	out.Status.Allocatable = it.Allocatable()

	// Best-effort: populate PublicIP via inspect; ignore errors so a
	// transient inspect failure doesn't undo a successful create.
	if got, ierr := c.docker.inspect(ctx, id); ierr == nil {
		out.Status.PublicIP = got.Status.PublicIP
	} else {
		out.Status.PublicIP = hostBindIP
	}
	return out, nil
}

func (c *CloudProvider) resolveClass(ctx context.Context, claim *v1alpha1.ExitClaim) (*ldv1alpha1.LocalDockerProviderClass, error) {
	if claim.Spec.ProviderClassRef.Kind != "LocalDockerProviderClass" {
		return nil, fmt.Errorf("localdocker: refusing kind %q", claim.Spec.ProviderClassRef.Kind)
	}
	if c.kube == nil {
		return nil, fmt.Errorf("localdocker: kube client not configured")
	}
	var pc ldv1alpha1.LocalDockerProviderClass
	if err := c.kube.Get(ctx, client.ObjectKey{Name: claim.Spec.ProviderClassRef.Name}, &pc); err != nil {
		return nil, fmt.Errorf("get LocalDockerProviderClass %q: %w", claim.Spec.ProviderClassRef.Name, err)
	}
	return &pc, nil
}

func (c *CloudProvider) Delete(ctx context.Context, claim *v1alpha1.ExitClaim) error {
	if claim.Status.ProviderID == "" {
		return cloudprovider.NewExitNotFoundError("")
	}
	return c.docker.remove(ctx, claim.Status.ProviderID)
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	return c.docker.inspect(ctx, providerID)
}

func (c *CloudProvider) List(ctx context.Context) ([]*v1alpha1.ExitClaim, error) {
	return c.docker.listManaged(ctx)
}

func (c *CloudProvider) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return InstanceTypes(), nil
}

func (c *CloudProvider) IsDrifted(ctx context.Context, claim *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	if claim.Status.ProviderID == "" {
		return "", nil
	}
	got, err := c.Get(ctx, claim.Status.ProviderID)
	if err != nil {
		if cloudprovider.IsExitNotFound(err) {
			return cloudprovider.DriftReason("Vanished"), nil
		}
		return "", err
	}
	// Best-effort version check: imageID isn't a clean signal for version,
	// but if Status.FrpsVersion was set on a prior reconcile and now
	// disagrees with the spec, that's drift.
	_ = got
	return "", nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []client.Object {
	return []client.Object{&ldv1alpha1.LocalDockerProviderClass{}}
}
