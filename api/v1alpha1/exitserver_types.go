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

// Provider identifies which Provisioner implementation manages an exit's VPS.
// +kubebuilder:validation:Enum=digitalocean;external;local-docker
type Provider string

const (
	ProviderDigitalOcean Provider = "digitalocean"
	ProviderExternal     Provider = "external"
	ProviderLocalDocker  Provider = "local-docker"
)

// ExitPhase is the lifecycle phase of an ExitServer.
// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Unreachable;Lost;Draining;Deleting
type ExitPhase string

const (
	PhasePending      ExitPhase = "Pending"
	PhaseProvisioning ExitPhase = "Provisioning"
	PhaseReady        ExitPhase = "Ready"
	PhaseDegraded     ExitPhase = "Degraded"
	PhaseUnreachable  ExitPhase = "Unreachable"
	PhaseLost         ExitPhase = "Lost"
	PhaseDraining     ExitPhase = "Draining"
	PhaseDeleting     ExitPhase = "Deleting"
)

// SecretKeyRef points at a key in a Secret in the same namespace as the referrer.
type SecretKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

type SSHConfig struct {
	// +kubebuilder:default=22
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

type FrpsConfig struct {
	// Pinned frps release tag; the operator ships a bundled checksum for it.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// +kubebuilder:default=7000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	BindPort int32 `json:"bindPort,omitempty"`

	// +kubebuilder:default=7500
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	AdminPort int32 `json:"adminPort,omitempty"`
}

// ExitCapacity declares per-exit caps used for reservation math.
// All fields optional; unset means "no limit on this dimension".
type ExitCapacity struct {
	// +kubebuilder:validation:Minimum=0
	MaxTunnels *int32 `json:"maxTunnels,omitempty"`
	// +kubebuilder:validation:Minimum=0
	MonthlyTrafficGB *int64 `json:"monthlyTrafficGB,omitempty"`
	// +kubebuilder:validation:Minimum=0
	BandwidthMbps *int32 `json:"bandwidthMbps,omitempty"`
}

type ExitServerSpec struct {
	Provider       Provider     `json:"provider"`
	Region         string       `json:"region,omitempty"`
	Size           string       `json:"size,omitempty"`
	CredentialsRef SecretKeyRef `json:"credentialsRef,omitempty"`
	SSH            SSHConfig    `json:"ssh,omitempty"`
	Frps           FrpsConfig   `json:"frps"`

	// AllowPorts is a list of port ranges (e.g. "1024-65535") or single ports
	// the operator may allocate to tunnels. Grow-only — admission webhook
	// rejects shrinks below current allocations (enforced in a later phase).
	// +kubebuilder:validation:MinItems=1
	AllowPorts []string `json:"allowPorts"`

	// ReservedPorts is a list of ports the operator must NOT allocate even if
	// they fall inside AllowPorts. Defaults to [SSH.Port, Frps.BindPort,
	// Frps.AdminPort] when empty (defaulted in webhook in a later phase).
	ReservedPorts []int32 `json:"reservedPorts,omitempty"`

	Capacity *ExitCapacity `json:"capacity,omitempty"`
}

// ExitUsage is the running sum of tunnel reservations on an exit.
type ExitUsage struct {
	Tunnels          int32 `json:"tunnels"`
	MonthlyTrafficGB int64 `json:"monthlyTrafficGB,omitempty"`
	BandwidthMbps    int32 `json:"bandwidthMbps,omitempty"`
}

type ExitServerStatus struct {
	Phase ExitPhase `json:"phase,omitempty"`

	PublicIP    string `json:"publicIP,omitempty"`
	ProviderID  string `json:"providerID,omitempty"`
	FrpsVersion string `json:"frpsVersion,omitempty"`

	// Allocations maps allocated public port (as string) to "namespace/name"
	// of the Tunnel that holds it. String keys because JSON object keys.
	Allocations map[string]string `json:"allocations,omitempty"`

	Usage ExitUsage `json:"usage,omitempty"`

	// Conditions is the standard Kubernetes condition list.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=exit;exits
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.status.publicIP`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Tunnels",type=integer,JSONPath=`.status.usage.tunnels`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ExitServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExitServerSpec   `json:"spec,omitempty"`
	Status ExitServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ExitServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExitServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExitServer{}, &ExitServerList{})
}
