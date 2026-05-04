package cloudprovider

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// CloudProvider is the per-provider contract. One implementation per
// ProviderClass kind. Resolved at runtime by Registry.
type CloudProvider interface {
	// Name identifies the provider (e.g. "local-docker", "digital-ocean").
	Name() string

	// Create launches a new exit. Implementations MUST return a hydrated
	// ExitClaim with Status.ProviderID, Status.ExitName, Status.Capacity,
	// Status.Allocatable, and Status.PublicIP populated when known.
	// On retry idempotency: if the provider already has an exit with
	// the same name, return the existing one (do not error).
	Create(ctx context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error)

	// Delete tears down the exit. Returns ExitNotFoundError when the
	// resource is already gone — caller stops retrying on that signal.
	Delete(ctx context.Context, claim *v1alpha1.ExitClaim) error

	// Get returns the live state of an exit by ProviderID. Used by drift
	// detection and consistency reconciliation.
	Get(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error)

	// List enumerates all exits the provider knows about (used for GC).
	List(ctx context.Context) ([]*v1alpha1.ExitClaim, error)

	// GetInstanceTypes returns the instance-type catalog for a given
	// ExitPool. Pool's requirements may filter the catalog.
	GetInstanceTypes(ctx context.Context, pool *v1alpha1.ExitPool) ([]*InstanceType, error)

	// IsDrifted compares live cloud state against the claim's spec.
	// Returns a non-empty DriftReason if the exit no longer matches.
	IsDrifted(ctx context.Context, claim *v1alpha1.ExitClaim) (DriftReason, error)

	// RepairPolicies declares which Node-condition-driven repairs the
	// provider supports. Empty for v1.
	RepairPolicies() []RepairPolicy

	// GetSupportedProviderClasses returns the ProviderClass CRD types
	// this provider accepts. Used by the operator to register schemes
	// and watchers.
	GetSupportedProviderClasses() []client.Object
}

// InstanceType is one provisionable shape exposed by a provider.
type InstanceType struct {
	// Name identifies the shape (e.g. "s-1vcpu-1gb").
	Name string
	// Requirements pins shape-specific labels (region, capacity-type, ...).
	Requirements []v1alpha1.NodeSelectorRequirementWithMinValues
	// Offerings is the per-zone-and-capacity-type variant list with prices.
	Offerings Offerings
	// Capacity is the full ResourceList this instance type provides.
	Capacity corev1.ResourceList
	// Overhead is the system reservation subtracted from Capacity to
	// produce Allocatable.
	Overhead corev1.ResourceList
}

// Allocatable returns Capacity minus Overhead. Helper.
func (i *InstanceType) Allocatable() corev1.ResourceList {
	out := corev1.ResourceList{}
	for k, v := range i.Capacity {
		out[k] = v.DeepCopy()
	}
	for k, v := range i.Overhead {
		if cur, ok := out[k]; ok {
			cur.Sub(v)
			out[k] = cur
		}
	}
	return out
}

// Offerings is a list of variants available for an InstanceType.
type Offerings []*Offering

// Offering is one zone-and-capacity-type variant of an instance type.
type Offering struct {
	Requirements []v1alpha1.NodeSelectorRequirementWithMinValues
	Price        float64
	Available    bool
}

// DriftReason is a non-empty string when the cloud-side state diverges
// from the declarative claim.
type DriftReason string

// RepairPolicy declares which Node condition triggers an auto-repair.
type RepairPolicy struct {
	ConditionType      string
	ConditionStatus    string
	TolerationDuration string
}

// NodeLifecycleHook is an optional extension. Providers may implement
// it to run custom logic at registration time (e.g. attach extra ENIs
// on AWS). Lifecycle controller invokes hooks after Registered=True.
type NodeLifecycleHook interface {
	Registered(ctx context.Context, claim *v1alpha1.ExitClaim) (NodeLifecycleHookResult, error)
}

type NodeLifecycleHookResult struct {
	// Requeue requests another reconcile after the named duration.
	Requeue *metav1Duration
}

// metav1Duration is a thin alias to avoid import cycles in tests.
type metav1Duration = v1alpha1.Duration
