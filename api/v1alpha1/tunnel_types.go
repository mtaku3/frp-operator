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

// +kubebuilder:validation:Enum=TCP;UDP
type TunnelProtocol string

const (
	ProtocolTCP TunnelProtocol = "TCP"
	ProtocolUDP TunnelProtocol = "UDP"
)

// +kubebuilder:validation:Enum=Never;OnOvercommit;OnExitLost;OnOvercommitOrLost
type MigrationPolicy string

const (
	MigrationNever              MigrationPolicy = "Never"
	MigrationOnOvercommit       MigrationPolicy = "OnOvercommit"
	MigrationOnExitLost         MigrationPolicy = "OnExitLost"
	MigrationOnOvercommitOrLost MigrationPolicy = "OnOvercommitOrLost"
)

// +kubebuilder:validation:Enum=Pending;Allocating;Provisioning;Connecting;Ready;Disconnected;Failed
type TunnelPhase string

const (
	TunnelPending      TunnelPhase = "Pending"
	TunnelAllocating   TunnelPhase = "Allocating"
	TunnelProvisioning TunnelPhase = "Provisioning"
	TunnelConnecting   TunnelPhase = "Connecting"
	TunnelReady        TunnelPhase = "Ready"
	TunnelDisconnected TunnelPhase = "Disconnected"
	TunnelFailed       TunnelPhase = "Failed"
)

type ServiceRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

type TunnelPort struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ServicePort int32 `json:"servicePort"`

	// PublicPort defaults to ServicePort when nil.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	PublicPort *int32 `json:"publicPort,omitempty"`

	// +kubebuilder:default=TCP
	Protocol TunnelProtocol `json:"protocol,omitempty"`
}

type ExitRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type Placement struct {
	Providers    []Provider `json:"providers,omitempty"`
	Regions      []string   `json:"regions,omitempty"`
	SizeOverride string     `json:"sizeOverride,omitempty"`
}

type PolicyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type TunnelRequirements struct {
	// +kubebuilder:validation:Minimum=0
	MonthlyTrafficGB *int64 `json:"monthlyTrafficGB,omitempty"`
	// +kubebuilder:validation:Minimum=0
	BandwidthMbps *int32 `json:"bandwidthMbps,omitempty"`
}

type TunnelSpec struct {
	Service ServiceRef `json:"service"`

	// +kubebuilder:validation:MinItems=1
	Ports []TunnelPort `json:"ports"`

	// ExitRef hard-pins the tunnel to a specific exit. If unset, the
	// scheduler picks (or provisions) one.
	ExitRef *ExitRef `json:"exitRef,omitempty"`

	// Placement is a soft preference list applied during (re-)allocation.
	// Ignored when ExitRef is set.
	Placement *Placement `json:"placement,omitempty"`

	SchedulingPolicyRef PolicyRef `json:"schedulingPolicyRef,omitempty"`

	Requirements *TunnelRequirements `json:"requirements,omitempty"`

	// +kubebuilder:default=Never
	MigrationPolicy MigrationPolicy `json:"migrationPolicy,omitempty"`

	// AllowPortSplit lets a multi-port tunnel land across multiple exits when
	// no single exit can host all ports. Default false → atomic placement.
	AllowPortSplit bool `json:"allowPortSplit,omitempty"`

	// ImmutableWhenReady locks rebind-triggering fields once status.phase
	// reaches Ready and disables autonomous migration regardless of
	// MigrationPolicy. Enforced by a validating webhook in a later phase.
	ImmutableWhenReady bool `json:"immutableWhenReady,omitempty"`
}

type TunnelStatus struct {
	Phase TunnelPhase `json:"phase,omitempty"`

	AssignedExit  string  `json:"assignedExit,omitempty"`
	AssignedIP    string  `json:"assignedIP,omitempty"`
	AssignedPorts []int32 `json:"assignedPorts,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=tn;tunnels
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.service.name`
// +kubebuilder:printcolumn:name="Exit",type=string,JSONPath=`.status.assignedExit`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.status.assignedIP`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
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

func init() {
	SchemeBuilder.Register(&Tunnel{}, &TunnelList{})
}
