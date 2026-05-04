/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tn
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name=Exit,type=string,JSONPath=`.status.assignedExit`
// +kubebuilder:printcolumn:name=IP,type=string,JSONPath=`.status.assignedIP`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type Tunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TunnelSpec   `json:"spec,omitempty"`
	Status TunnelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tunnel `json:"items"`
}

type TunnelSpec struct {
	Ports        []TunnelPort                           `json:"ports"`
	Requirements []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
	// ExitClaimRef hard-pins the tunnel to a specific ExitClaim, bypassing
	// the scheduler.
	// +optional
	ExitClaimRef *LocalObjectReference `json:"exitClaimRef,omitempty"`
	// Resources.Requests is the binpack input. Optional; empty fits anywhere.
	// +optional
	Resources ResourceRequirements `json:"resources,omitempty"`
}

type TunnelPort struct {
	// Name is a free-form identifier; required when there is more than one port.
	// +optional
	Name string `json:"name,omitempty"`
	// PublicPort is the requested public port on the exit. 0 means
	// auto-assign from AllowPorts.
	// +optional
	PublicPort *int32 `json:"publicPort,omitempty"`
	// ServicePort is the in-cluster port frpc forwards traffic from.
	ServicePort int32 `json:"servicePort"`
	// Protocol is "TCP" (default) or "UDP".
	// +kubebuilder:default=TCP
	// +kubebuilder:validation:Enum=TCP;UDP
	Protocol string `json:"protocol,omitempty"`
}

type TunnelPhase string

const (
	TunnelPhasePending      TunnelPhase = ""
	TunnelPhaseAllocating   TunnelPhase = "Allocating"
	TunnelPhaseProvisioning TunnelPhase = "Provisioning"
	TunnelPhaseReady        TunnelPhase = "Ready"
	TunnelPhaseFailed       TunnelPhase = "Failed"
)

type TunnelStatus struct {
	// Phase is a coarse summary of Conditions.
	// +optional
	Phase TunnelPhase `json:"phase,omitempty"`
	// AssignedExit is the name of the ExitClaim this tunnel is bound to.
	// +optional
	AssignedExit string `json:"assignedExit,omitempty"`
	// AssignedIP is the public IP of the assigned ExitClaim.
	// +optional
	AssignedIP string `json:"assignedIP,omitempty"`
	// AssignedPorts are the resolved public port numbers (auto-assigned
	// values filled in for PublicPort=0 inputs).
	// +optional
	AssignedPorts []int32 `json:"assignedPorts,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Tunnel{}, &TunnelList{})
}
