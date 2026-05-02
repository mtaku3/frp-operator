/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

	// AllowPorts seeds new ExitServer.spec.allowPorts. A tunnel whose
	// requested public ports fall outside this set will not provision a
	// new exit; instead, the tunnel stays Allocating until either an
	// existing eligible exit appears or the policy default is widened.
	// When empty, the operator falls back to "1024-65535".
	AllowPorts []string `json:"allowPorts,omitempty"`
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
