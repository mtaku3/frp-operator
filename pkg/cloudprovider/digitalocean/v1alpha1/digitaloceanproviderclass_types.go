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
	// Regions is the discovery set of DO region slugs (nyc3, sfo3, ...).
	// Karpenter NodeClass equivalent: subnetSelectorTerms — declares the
	// candidate set; the scheduler picks one per claim by pinning
	// topology.kubernetes.io/region in claim.Spec.Requirements.
	// +kubebuilder:validation:MinItems=1
	Regions []string `json:"regions"`
	// Sizes is the discovery set of DO droplet size slugs. Scheduler
	// picks the cheapest size that satisfies the bound tunnels' resource
	// requests, and pins node.kubernetes.io/instance-type on the claim.
	// +kubebuilder:validation:MinItems=1
	Sizes []string `json:"sizes"`
	// ImageSelectorTerms is the discovery set of acceptable droplet
	// base images. Karpenter NodeClass equivalent: amiSelectorTerms.
	// Scheduler picks one image per claim from the matched set; with
	// only Slug supported today the selection reduces to "first item".
	// Future arch-aware claims will narrow by image arch.
	// +kubebuilder:validation:MinItems=1
	ImageSelectorTerms []ImageSelectorTerm `json:"imageSelectorTerms"`
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
	// TODO(phase9): regenerate CRD with proper binary-URL default in DefaultImage
	// +kubebuilder:default="fatedier/frps:%s"
	DefaultImage string `json:"defaultImage,omitempty"`
}

// ImageSelectorTerm matches one or more DO base images. Mirrors
// karpenter's AMISelectorTerm: a struct with multiple match modes so
// future selectors (alias, tags) can be added without a CRD bump.
// At least one of Slug or Alias must be set; an empty term is invalid.
type ImageSelectorTerm struct {
	// Slug is the exact DO image slug (e.g. ubuntu-22-04-x64).
	// +optional
	Slug string `json:"slug,omitempty"`
	// Alias is a moving-tag-style shorthand (e.g. ubuntu@22.04).
	// Resolution is provider-side. Reserved for future use.
	// +optional
	Alias string `json:"alias,omitempty"`
}

type DigitalOceanProviderClassStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&DigitalOceanProviderClass{}, &DigitalOceanProviderClassList{})
}
