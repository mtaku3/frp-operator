/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ldpc
// +kubebuilder:subresource:status
type LocalDockerProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LocalDockerProviderClassSpec   `json:"spec,omitempty"`
	Status LocalDockerProviderClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type LocalDockerProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LocalDockerProviderClass `json:"items"`
}

type LocalDockerProviderClassSpec struct {
	// Network is the docker network name; localdocker exits attach here.
	// +kubebuilder:default=kind
	Network string `json:"network,omitempty"`
	// ConfigHostMountPath is the host directory bind-mounted into each
	// frps container so the operator can write frps.toml. Must exist
	// and be world-writable on every kind node.
	// +kubebuilder:default="/tmp/frp-operator-shared"
	ConfigHostMountPath string `json:"configHostMountPath,omitempty"`
	// ImagePullPolicy controls whether containers always re-pull.
	// +kubebuilder:default=IfNotPresent
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
	// SkipHostPortPublishing disables -p hostPort:containerPort flags;
	// useful for kind e2e where multiple exits share host ports.
	// +optional
	SkipHostPortPublishing bool `json:"skipHostPortPublishing,omitempty"`
	// DefaultImage is the base frps container image template, e.g.
	// "fatedier/frps:%s" — version substituted at launch.
	// +kubebuilder:default="fatedier/frps:%s"
	DefaultImage string `json:"defaultImage,omitempty"`
}

type LocalDockerProviderClassStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&LocalDockerProviderClass{}, &LocalDockerProviderClassList{})
}
