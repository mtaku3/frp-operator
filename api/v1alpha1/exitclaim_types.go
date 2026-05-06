/*
Copyright 2026.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ec
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Pool,type=string,JSONPath=`.metadata.labels.frp\.operator\.io/exitpool`
// +kubebuilder:printcolumn:name=Provider,type=string,JSONPath=`.spec.providerClassRef.kind`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=PublicIP,type=string,JSONPath=`.status.publicIP`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type ExitClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExitClaimSpec   `json:"spec,omitempty"`
	Status ExitClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ExitClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExitClaim `json:"items"`
}

type ExitClaimSpec struct {
	ProviderClassRef ProviderClassRef `json:"providerClassRef"`
	// +optional
	Requirements []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
	Frps         FrpsConfig                             `json:"frps"`
	// Resources.Requests is controller-derived: sum of bound tunnel
	// requests + frps overhead. Users do NOT set this directly.
	// +optional
	Resources ResourceRequirements `json:"resources,omitempty"`
	// +optional
	ExpireAfter Duration `json:"expireAfter,omitempty"`
	// +optional
	TerminationGracePeriod *Duration `json:"terminationGracePeriod,omitempty"`
}

type ExitClaimStatus struct {
	// ProviderID is the cloud-provider identifier (e.g.
	// localdocker://<container-id>, do://<droplet-id>).
	// +optional
	ProviderID string `json:"providerID,omitempty"`
	// +optional
	PublicIP string `json:"publicIP,omitempty"`
	// ExitName is the provider-side resource name (container, droplet).
	// +optional
	ExitName string `json:"exitName,omitempty"`
	// ImageID is the provider's image identifier (container digest,
	// droplet base image). Mirrors Karpenter NodeClaim.Status.ImageID.
	// +optional
	ImageID string `json:"imageID,omitempty"`
	// FrpsVersion is the actually-running frps binary version, reported
	// by admin API after Registration.
	// +optional
	FrpsVersion string `json:"frpsVersion,omitempty"`
	// Capacity is the estimated full capacity of the exit. Populated
	// from cloudProvider.Create return value. ESTIMATE only — same
	// convention as Karpenter NodeClaim.Status.Capacity.
	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`
	// Allocatable is the estimated allocatable capacity.
	// +optional
	Allocatable corev1.ResourceList `json:"allocatable,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// NOTE: there is no Allocations field. Truth lives on each Tunnel's
	// Status.AssignedExit + Status.AssignedPorts. state.StateExit
	// aggregates them in-memory.
}

func init() {
	SchemeBuilder.Register(&ExitClaim{}, &ExitClaimList{})
}
