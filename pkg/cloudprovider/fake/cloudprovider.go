package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

var _ cloudprovider.CloudProvider = (*CloudProvider)(nil)

// CloudProvider is the in-memory fake impl. Goroutine-safe.
type CloudProvider struct {
	mu        sync.RWMutex
	exits     map[string]*v1alpha1.ExitClaim // keyed by ProviderID
	driftMap  map[string]cloudprovider.DriftReason
	instances []*cloudprovider.InstanceType
	// Hooks for tests to inject failures.
	CreateFailure error
	DeleteFailure error
}

// New constructs a fake with default instance types.
func New() *CloudProvider {
	return &CloudProvider{
		exits:     map[string]*v1alpha1.ExitClaim{},
		driftMap:  map[string]cloudprovider.DriftReason{},
		instances: DefaultInstanceTypes(),
	}
}

func (c *CloudProvider) Name() string { return "fake" }

func (c *CloudProvider) Create(_ context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	if c.CreateFailure != nil {
		return nil, c.CreateFailure
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Idempotency: same name → same ProviderID.
	for id, existing := range c.exits {
		if existing.Name == claim.Name {
			return c.cloneAndHydrate(claim, id), nil
		}
	}
	id := "fake://" + uuid.NewString()
	hydrated := c.cloneAndHydrate(claim, id)
	c.exits[id] = hydrated.DeepCopy()
	return hydrated, nil
}

func (c *CloudProvider) cloneAndHydrate(claim *v1alpha1.ExitClaim, providerID string) *v1alpha1.ExitClaim {
	out := claim.DeepCopy()
	out.Status.ProviderID = providerID
	out.Status.ExitName = "fake-exit-" + claim.Name
	out.Status.PublicIP = "203.0.113.1"
	out.Status.ImageID = "fake-image:" + claim.Spec.Frps.Version
	out.Status.FrpsVersion = claim.Spec.Frps.Version
	out.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("1Gi"),
		corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("1000"),
	}
	out.Status.Allocatable = out.Status.Capacity.DeepCopy()
	return out
}

func (c *CloudProvider) Delete(_ context.Context, claim *v1alpha1.ExitClaim) error {
	if c.DeleteFailure != nil {
		return c.DeleteFailure
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.exits[claim.Status.ProviderID]; !ok {
		return cloudprovider.NewExitNotFoundError(claim.Status.ProviderID)
	}
	delete(c.exits, claim.Status.ProviderID)
	return nil
}

func (c *CloudProvider) Get(_ context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	got, ok := c.exits[providerID]
	if !ok {
		return nil, cloudprovider.NewExitNotFoundError(providerID)
	}
	return got.DeepCopy(), nil
}

func (c *CloudProvider) List(_ context.Context) ([]*v1alpha1.ExitClaim, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*v1alpha1.ExitClaim, 0, len(c.exits))
	for _, e := range c.exits {
		out = append(out, e.DeepCopy())
	}
	return out, nil
}

func (c *CloudProvider) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return c.instances, nil
}

func (c *CloudProvider) IsDrifted(_ context.Context, claim *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.driftMap[claim.Status.ProviderID], nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []client.Object {
	return []client.Object{&FakeProviderClass{}}
}

// MarkDrifted is a test helper.
func (c *CloudProvider) MarkDrifted(providerID string, reason cloudprovider.DriftReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.driftMap[providerID] = reason
}

// Reset wipes all stored exits/drift. Useful between tests.
func (c *CloudProvider) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exits = map[string]*v1alpha1.ExitClaim{}
	c.driftMap = map[string]cloudprovider.DriftReason{}
}

// ErrorOnCreate is a fmt-style helper to inject a typed CreateFailure during a test.
func (c *CloudProvider) ErrorOnCreate(format string, args ...interface{}) {
	c.CreateFailure = fmt.Errorf(format, args...)
}
