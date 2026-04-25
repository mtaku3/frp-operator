package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AllocatorName names a built-in or registered Allocator. Custom
// allocators use the form "Custom:<name>" — validation accepts any
// non-empty string; resolution to a concrete impl happens at runtime.
type AllocatorName string

const (
	AllocatorBinPack       AllocatorName = "BinPack"
	AllocatorSpread        AllocatorName = "Spread"
	AllocatorCapacityAware AllocatorName = "CapacityAware"
)

type ProvisionerName string

const (
	ProvisionerOnDemand  ProvisionerName = "OnDemand"
	ProvisionerFixedPool ProvisionerName = "FixedPool"
)

type BudgetSpec struct {
	// +kubebuilder:validation:Minimum=0
	MaxExits *int32 `json:"maxExits,omitempty"`
	// +kubebuilder:validation:Minimum=0
	MaxExitsPerNamespace *int32 `json:"maxExitsPerNamespace,omitempty"`
}

type VPSDefaults struct {
	Provider Provider      `json:"provider,omitempty"`
	Regions  []string      `json:"regions,omitempty"`
	Size     string        `json:"size,omitempty"`
	Capacity *ExitCapacity `json:"capacity,omitempty"`
}

type VPSSpec struct {
	Default VPSDefaults `json:"default,omitempty"`
}

type ConsolidationSpec struct {
	// +kubebuilder:default=true
	ReclaimEmpty bool `json:"reclaimEmpty,omitempty"`

	// +kubebuilder:default="10m"
	DrainAfter metav1.Duration `json:"drainAfter,omitempty"`
}

type ProbesSpec struct {
	// +kubebuilder:default="30s"
	AdminInterval metav1.Duration `json:"adminInterval,omitempty"`
	// +kubebuilder:default="5m"
	ProviderInterval metav1.Duration `json:"providerInterval,omitempty"`
	// +kubebuilder:default="5m"
	DegradedTimeout metav1.Duration `json:"degradedTimeout,omitempty"`
	// +kubebuilder:default="5m"
	LostGracePeriod metav1.Duration `json:"lostGracePeriod,omitempty"`
}

type SchedulingPolicySpec struct {
	// +kubebuilder:default=CapacityAware
	// +kubebuilder:validation:MinLength=1
	Allocator AllocatorName `json:"allocator,omitempty"`

	// +kubebuilder:default=OnDemand
	// +kubebuilder:validation:MinLength=1
	Provisioner ProvisionerName `json:"provisioner,omitempty"`

	Budget        BudgetSpec        `json:"budget,omitempty"`
	VPS           VPSSpec           `json:"vps,omitempty"`
	Consolidation ConsolidationSpec `json:"consolidation,omitempty"`
	Probes        ProbesSpec        `json:"probes,omitempty"`
}

type SchedulingPolicyStatus struct {
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=spol
// +kubebuilder:printcolumn:name="Allocator",type=string,JSONPath=`.spec.allocator`
// +kubebuilder:printcolumn:name="Provisioner",type=string,JSONPath=`.spec.provisioner`
// +kubebuilder:printcolumn:name="MaxExits",type=integer,JSONPath=`.spec.budget.maxExits`
type SchedulingPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SchedulingPolicySpec   `json:"spec,omitempty"`
	Status SchedulingPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SchedulingPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SchedulingPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SchedulingPolicy{}, &SchedulingPolicyList{})
}
