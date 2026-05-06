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

var _ cloudprovider.CloudProvider = (*CloudProvider)(nil)

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

func (c *CloudProvider) resolveClass(
	ctx context.Context, claim *v1alpha1.ExitClaim,
) (*dov1alpha1.DigitalOceanProviderClass, error) {
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

// firstRequirementValue returns the first Values[0] for a requirement
// key with operator In. Karpenter pins exactly one value per chosen
// dimension (instance-type, region) so taking [0] is safe.
func firstRequirementValue(reqs []v1alpha1.NodeSelectorRequirementWithMinValues, key string) string {
	for _, r := range reqs {
		if r.Key != key {
			continue
		}
		if r.Operator != v1alpha1.NodeSelectorOpIn {
			continue
		}
		if len(r.Values) == 0 {
			continue
		}
		return r.Values[0]
	}
	return ""
}

// resolveImage returns the droplet image slug to launch. With only
// Slug supported on ImageSelectorTerm today, this reduces to "first
// term's Slug". Phase B will arch-narrow when claims pin
// kubernetes.io/arch in their Requirements.
func resolveImage(pc *dov1alpha1.DigitalOceanProviderClass) (string, error) {
	for _, t := range pc.Spec.ImageSelectorTerms {
		if t.Slug != "" {
			return t.Slug, nil
		}
	}
	return "", fmt.Errorf("digitalocean: providerclass %q has no image selector with slug", pc.Name)
}

// resolveSelection extracts the size + region the scheduler chose for
// this claim. Falls back to the first entry of the ProviderClass
// discovery set when the scheduler hasn't pinned (lets Phase A1 ship
// before A2 wires real offering selection). Returns ("", "", err) if
// neither claim nor class names a valid choice.
func resolveSelection(
	claim *v1alpha1.ExitClaim, pc *dov1alpha1.DigitalOceanProviderClass,
) (size, region string, err error) {
	size = firstRequirementValue(claim.Spec.Requirements, v1alpha1.RequirementInstanceType)
	region = firstRequirementValue(claim.Spec.Requirements, v1alpha1.RequirementRegion)
	if size == "" {
		if len(pc.Spec.Sizes) == 0 {
			return "", "", fmt.Errorf("digitalocean: providerclass %q has no sizes", pc.Name)
		}
		size = pc.Spec.Sizes[0]
	}
	if region == "" {
		if len(pc.Spec.Regions) == 0 {
			return "", "", fmt.Errorf("digitalocean: providerclass %q has no regions", pc.Name)
		}
		region = pc.Spec.Regions[0]
	}
	return size, region, nil
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

	imageSlug, err := resolveImage(pc)
	if err != nil {
		return nil, err
	}

	size, region, err := resolveSelection(claim, pc)
	if err != nil {
		return nil, err
	}

	req := &godo.DropletCreateRequest{
		Name:       name,
		Region:     region,
		Size:       size,
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

func (c *CloudProvider) hydrate(
	claim *v1alpha1.ExitClaim, pc *dov1alpha1.DigitalOceanProviderClass, d *godo.Droplet,
) *v1alpha1.ExitClaim {
	out := claim.DeepCopy()
	out.Status.ProviderID = providerIDFor(d.ID)
	out.Status.ExitName = d.Name
	out.Status.PublicIP = publicIPv4(d)
	if img, err := resolveImage(pc); err == nil {
		out.Status.ImageID = img
	}
	out.Status.FrpsVersion = claim.Spec.Frps.Version
	// Prefer the live droplet size (DO is the source of truth post-create);
	// fall back to the scheduler-pinned requirement when Size is unset.
	sizeSlug := ""
	if d.Size != nil {
		sizeSlug = d.Size.Slug
	}
	if sizeSlug == "" {
		sizeSlug = firstRequirementValue(claim.Spec.Requirements, v1alpha1.RequirementInstanceType)
	}
	if it := instanceTypeBySize(sizeSlug); it != nil {
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
// TODO(phase5): resolve auth token via state.Cluster + ProviderClass lookup
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

// TODO(phase5): resolve auth token via state.Cluster + ProviderClass lookup
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

// GetInstanceTypes returns the discovered catalog for a pool. Karpenter
// NodeClass equivalent: subnetSelectorTerms+amiSelectorTerms intersection.
// Here: the cross-product of pc.Spec.Sizes × pc.Spec.Regions, narrowed to
// what the operator's static catalog actually supports. Returned slice
// is the candidate set the scheduler will sort by Offering.Price.
func (c *CloudProvider) GetInstanceTypes(
	ctx context.Context, pool *v1alpha1.ExitPool,
) ([]*cloudprovider.InstanceType, error) {
	if pool == nil {
		return InstanceTypes(), nil
	}
	if pool.Spec.Template.Spec.ProviderClassRef.Kind != "DigitalOceanProviderClass" {
		return nil, fmt.Errorf("digitalocean: pool %q references kind %q",
			pool.Name, pool.Spec.Template.Spec.ProviderClassRef.Kind)
	}
	if c.kube == nil {
		return InstanceTypes(), nil
	}
	var pc dov1alpha1.DigitalOceanProviderClass
	if err := c.kube.Get(ctx, client.ObjectKey{Name: pool.Spec.Template.Spec.ProviderClassRef.Name}, &pc); err != nil {
		return nil, fmt.Errorf("get DigitalOceanProviderClass %q: %w",
			pool.Spec.Template.Spec.ProviderClassRef.Name, err)
	}
	return FilteredInstanceTypes(pc.Spec.Sizes, pc.Spec.Regions), nil
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
	// Drift compares the live droplet against the discovery set declared
	// on the ProviderClass. A droplet whose size or region is no longer
	// in the allowed lists has drifted; the disruption controller will
	// then mint a replacement and the new claim's Requirements will pin
	// a fresh in-set choice.
	if d.Size != nil && !contains(pc.Spec.Sizes, d.Size.Slug) {
		return cloudprovider.DriftReason("SizeMismatch"), nil
	}
	if d.Region != nil && !contains(pc.Spec.Regions, d.Region.Slug) {
		return cloudprovider.DriftReason("RegionMismatch"), nil
	}
	return "", nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []client.Object {
	return []client.Object{&dov1alpha1.DigitalOceanProviderClass{}}
}
