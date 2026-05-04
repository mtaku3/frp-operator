/*
Copyright 2026.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=dopc
// +kubebuilder:subresource:status
type DigitalOceanProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DigitalOceanProviderClassSpec   `json:"spec,omitempty"`
	Status DigitalOceanProviderClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DigitalOceanProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DigitalOceanProviderClass `json:"items"`
}

type DigitalOceanProviderClassSpec struct {
	// APITokenSecretRef references the Secret holding a DO API token.
	APITokenSecretRef frpv1alpha1.SecretKeyRef `json:"apiTokenSecretRef"`
	// Region is the DO region slug (nyc3, sfo3, ...).
	Region string `json:"region"`
	// Size is the droplet size slug (s-1vcpu-1gb, ...).
	Size string `json:"size"`
	// ImageID is the droplet base image; default ubuntu-22-04-x64.
	// +kubebuilder:default="ubuntu-22-04-x64"
	ImageID string `json:"imageID,omitempty"`
	// VPCUUID optionally pins the VPC.
	// +optional
	VPCUUID string `json:"vpcUUID,omitempty"`
	// SSHKeyIDs lists DO ssh key IDs to inject.
	// +optional
	SSHKeyIDs []string `json:"sshKeyIDs,omitempty"`
	// Monitoring enables DO monitoring agent.
	// +optional
	Monitoring bool `json:"monitoring,omitempty"`
	// DefaultImage is the frps binary download URL template used by
	// cloud-init. DigitalOcean provisions plain VMs (cloud-init), NOT
	// containers, so this MUST be a binary URL like
	// "https://github.com/fatedier/frp/releases/download/%s/frp_%s_linux_amd64.tar.gz".
	// The historic default value below is a container reference and is
	// retained for CRD compatibility; cloud-init substitutes the URL
	// template when this looks non-URL. The default fix is deferred to
	// Phase 9 (requires CRD regeneration).
	// +kubebuilder:default="fatedier/frps:%s"
	DefaultImage string `json:"defaultImage,omitempty"`
}

type DigitalOceanProviderClassStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&DigitalOceanProviderClass{}, &DigitalOceanProviderClassList{})
}
