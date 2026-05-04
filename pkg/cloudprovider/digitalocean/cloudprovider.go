// Package digitalocean implements cloudprovider.CloudProvider against
// the DigitalOcean API.
package digitalocean

import (
	"context"
	"fmt"
	"net/url"

	"github.com/digitalocean/godo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
)

// CloudProvider is the DO impl. Constructs a godo.Client per-Create
// using the API token resolved from the ProviderClass.
type CloudProvider struct {
	kube client.Client

	// BaseURL overrides the godo default endpoint; tests point at httptest.
	BaseURL string

	// AuthTokenResolver fetches the frps auth token for a claim. If nil,
	// an empty token is used.
	AuthTokenResolver func(ctx context.Context, claim *v1alpha1.ExitClaim) (string, error)

	// APITokenResolver fetches the DO API token from the
	// ProviderClass.Spec.APITokenSecretRef. If nil, callers MUST pass a
	// pre-built godo client via NewWithClient.
	APITokenResolver func(ctx context.Context, pc *dov1alpha1.DigitalOceanProviderClass) (string, error)

	// dropletAPIFactory builds a dropletAPI from a token. Tests inject
	// stubs by overriding this.
	dropletAPIFactory func(ctx context.Context, token string) (dropletAPI, error)
}

// New constructs a DO CloudProvider. baseURL is optional ("" → DO prod).
func New(kube client.Client, baseURL string) (*CloudProvider, error) {
	c := &CloudProvider{kube: kube, BaseURL: baseURL}
	c.dropletAPIFactory = c.defaultDropletAPI
	return c, nil
}

func (c *CloudProvider) defaultDropletAPI(_ context.Context, token string) (dropletAPI, error) {
	if token == "" {
		return nil, fmt.Errorf("digitalocean: empty API token")
	}
	gc := godo.NewFromToken(token)
	if c.BaseURL != "" {
		base := c.BaseURL
		if base[len(base)-1] != '/' {
			base += "/"
		}
		u, err := url.Parse(base)
		if err != nil {
			return nil, fmt.Errorf("parse BaseURL: %w", err)
		}
		gc.BaseURL = u
	}
	return gc.Droplets, nil
}

// SetDropletAPIFactory overrides the godo factory; intended for tests.
func (c *CloudProvider) SetDropletAPIFactory(f func(ctx context.Context, token string) (dropletAPI, error)) {
	c.dropletAPIFactory = f
}

func (c *CloudProvider) Name() string { return "digital-ocean" }

func (c *CloudProvider) resolveClass(ctx context.Context, claim *v1alpha1.ExitClaim) (*dov1alpha1.DigitalOceanProviderClass, error) {
	if claim.Spec.ProviderClassRef.Kind != "DigitalOceanProviderClass" {
		return nil, fmt.Errorf("digitalocean: refusing kind %q", claim.Spec.ProviderClassRef.Kind)
	}
	if c.kube == nil {
		return nil, fmt.Errorf("digitalocean: kube client not configured")
	}
	var pc dov1alpha1.DigitalOceanProviderClass
	if err := c.kube.Get(ctx, client.ObjectKey{Name: claim.Spec.ProviderClassRef.Name}, &pc); err != nil {
		return nil, fmt.Errorf("get DigitalOceanProviderClass %q: %w", claim.Spec.ProviderClassRef.Name, err)
	}
	return &pc, nil
}

func (c *CloudProvider) apiFor(ctx context.Context, pc *dov1alpha1.DigitalOceanProviderClass) (dropletAPI, error) {
	token := ""
	if c.APITokenResolver != nil {
		t, err := c.APITokenResolver(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("resolve DO API token: %w", err)
		}
		token = t
	}
	return c.dropletAPIFactory(ctx, token)
}

// instanceTypeBySize returns the static catalog entry for a slug, or
// nil if unknown.
func instanceTypeBySize(slug string) *cloudprovider.InstanceType {
	for _, it := range InstanceTypes() {
		if it.Name == slug {
			return it
		}
	}
	return nil
}

func (c *CloudProvider) Create(ctx context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	pc, err := c.resolveClass(ctx, claim)
	if err != nil {
		return nil, err
	}
	api, err := c.apiFor(ctx, pc)
	if err != nil {
		return nil, err
	}

	name := dropletNameFor(claim)

	// Idempotency: name lookup, then tag scoping.
	if existing, err := findByName(ctx, api, name); err != nil {
		return nil, err
	} else if existing != nil {
		return c.hydrate(claim, pc, existing), nil
	}

	authToken := ""
	if c.AuthTokenResolver != nil {
		authToken, err = c.AuthTokenResolver(ctx, claim)
		if err != nil {
			return nil, fmt.Errorf("resolve auth token: %w", err)
		}
	}
	userData, err := RenderCloudInit(claim.Spec.Frps, authToken, pc.Spec.DefaultImage)
	if err != nil {
		return nil, fmt.Errorf("render cloud-init: %w", err)
	}

	imageSlug := pc.Spec.ImageID
	if imageSlug == "" {
		imageSlug = "ubuntu-22-04-x64"
	}

	req := &godo.DropletCreateRequest{
		Name:       name,
		Region:     pc.Spec.Region,
		Size:       pc.Spec.Size,
		Image:      godo.DropletCreateImage{Slug: imageSlug},
		UserData:   userData,
		Tags:       []string{ManagedTag},
		VPCUUID:    pc.Spec.VPCUUID,
		Monitoring: pc.Spec.Monitoring,
	}
	if len(pc.Spec.SSHKeyIDs) > 0 {
		keys := make([]godo.DropletCreateSSHKey, 0, len(pc.Spec.SSHKeyIDs))
		for _, k := range pc.Spec.SSHKeyIDs {
			keys = append(keys, godo.DropletCreateSSHKey{Fingerprint: k})
		}
		req.SSHKeys = keys
	}

	d, _, err := api.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create droplet: %w", err)
	}
	return c.hydrate(claim, pc, d), nil
}

func (c *CloudProvider) hydrate(claim *v1alpha1.ExitClaim, pc *dov1alpha1.DigitalOceanProviderClass, d *godo.Droplet) *v1alpha1.ExitClaim {
	out := claim.DeepCopy()
	out.Status.ProviderID = providerIDFor(d.ID)
	out.Status.ExitName = d.Name
	out.Status.PublicIP = publicIPv4(d)
	out.Status.ImageID = pc.Spec.ImageID
	out.Status.FrpsVersion = claim.Spec.Frps.Version
	if it := instanceTypeBySize(pc.Spec.Size); it != nil {
		out.Status.Capacity = it.Capacity.DeepCopy()
		out.Status.Allocatable = it.Allocatable()
	} else {
		out.Status.Capacity = corev1.ResourceList{}
		out.Status.Allocatable = corev1.ResourceList{}
	}
	return out
}

func (c *CloudProvider) Delete(ctx context.Context, claim *v1alpha1.ExitClaim) error {
	if claim.Status.ProviderID == "" {
		return cloudprovider.NewExitNotFoundError("")
	}
	pc, err := c.resolveClass(ctx, claim)
	if err != nil {
		return err
	}
	api, err := c.apiFor(ctx, pc)
	if err != nil {
		return err
	}
	return dropletDelete(ctx, api, claim.Status.ProviderID)
}

// Get returns a hydrated ExitClaim for a providerID. NOTE: without a
// ProviderClass we can't authenticate — this method requires the caller
// to have already plumbed APITokenResolver to a per-class default. For
// drift detection, callers SHOULD use IsDrifted (which receives the
// claim with a ref) rather than Get directly.
func (c *CloudProvider) Get(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	api, err := c.dropletAPIFactory(ctx, "")
	if err != nil {
		return nil, err
	}
	d, err := dropletGet(ctx, api, providerID)
	if err != nil {
		return nil, err
	}
	out := &v1alpha1.ExitClaim{}
	out.Status.ProviderID = providerIDFor(d.ID)
	out.Status.ExitName = d.Name
	out.Status.PublicIP = publicIPv4(d)
	return out, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*v1alpha1.ExitClaim, error) {
	api, err := c.dropletAPIFactory(ctx, "")
	if err != nil {
		return nil, err
	}
	droplets, err := listManaged(ctx, api)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.ExitClaim, 0, len(droplets))
	for i := range droplets {
		d := &droplets[i]
		out = append(out, &v1alpha1.ExitClaim{
			Status: v1alpha1.ExitClaimStatus{
				ProviderID: providerIDFor(d.ID),
				ExitName:   d.Name,
				PublicIP:   publicIPv4(d),
			},
		})
	}
	return out, nil
}

func (c *CloudProvider) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return InstanceTypes(), nil
}

func (c *CloudProvider) IsDrifted(ctx context.Context, claim *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	if claim.Status.ProviderID == "" {
		return "", nil
	}
	pc, err := c.resolveClass(ctx, claim)
	if err != nil {
		return "", err
	}
	api, err := c.apiFor(ctx, pc)
	if err != nil {
		return "", err
	}
	d, err := dropletGet(ctx, api, claim.Status.ProviderID)
	if err != nil {
		if cloudprovider.IsExitNotFound(err) {
			return cloudprovider.DriftReason("Vanished"), nil
		}
		return "", err
	}
	if d.Size != nil && d.Size.Slug != pc.Spec.Size {
		return cloudprovider.DriftReason("SizeMismatch"), nil
	}
	if d.Region != nil && d.Region.Slug != pc.Spec.Region {
		return cloudprovider.DriftReason("RegionMismatch"), nil
	}
	return "", nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []client.Object {
	return []client.Object{&dov1alpha1.DigitalOceanProviderClass{}}
}
