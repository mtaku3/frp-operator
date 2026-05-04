/*
Copyright 2026.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ep
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Exits,type=integer,JSONPath=`.status.exits`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type ExitPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExitPoolSpec   `json:"spec,omitempty"`
	Status ExitPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ExitPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExitPool `json:"items"`
}

type ExitPoolSpec struct {
	Template ExitClaimTemplate `json:"template"`
	// +optional
	Disruption Disruption `json:"disruption,omitempty"`
	// +optional
	Limits Limits `json:"limits,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Weight *int32 `json:"weight,omitempty"`
	// Replicas enables static-mode (alpha, gated by feature gate
	// StaticReplicas). When set, the operator maintains exactly N
	// ExitClaims regardless of demand.
	// +optional
	Replicas *int64 `json:"replicas,omitempty"`
}

type ExitClaimTemplate struct {
	// +optional
	Metadata ExitClaimTemplateMetadata `json:"metadata,omitempty"`
	Spec     ExitClaimTemplateSpec     `json:"spec"`
}

type ExitClaimTemplateMetadata struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ExitClaimTemplateSpec deliberately omits Resources — per Karpenter
// convention, users cannot pre-allocate per-claim resource requests on
// the pool template. Scheduler fills ExitClaim.Spec.Resources.Requests
// at provision time.
type ExitClaimTemplateSpec struct {
	ProviderClassRef ProviderClassRef `json:"providerClassRef"`
	// +optional
	Requirements []NodeSelectorRequirementWithMinValues `json:"requirements,omitempty"`
	Frps         FrpsConfig                             `json:"frps"`
	// +optional
	ExpireAfter Duration `json:"expireAfter,omitempty"`
	// +optional
	TerminationGracePeriod *Duration `json:"terminationGracePeriod,omitempty"`
}

type Disruption struct {
	// +kubebuilder:validation:Enum=WhenEmpty;WhenEmptyOrUnderutilized
	// +optional
	ConsolidationPolicy ConsolidationPolicy `json:"consolidationPolicy,omitempty"`
	// +optional
	ConsolidateAfter Duration `json:"consolidateAfter,omitempty"`
	// +optional
	Budgets []DisruptionBudget `json:"budgets,omitempty"`
}

type ConsolidationPolicy string

const (
	ConsolidationWhenEmpty                ConsolidationPolicy = "WhenEmpty"
	ConsolidationWhenEmptyOrUnderutilized ConsolidationPolicy = "WhenEmptyOrUnderutilized"
)

type DisruptionReason string

const (
	DisruptionReasonEmpty         DisruptionReason = "Empty"
	DisruptionReasonDrifted       DisruptionReason = "Drifted"
	DisruptionReasonExpired       DisruptionReason = "Expired"
	DisruptionReasonUnderutilized DisruptionReason = "Underutilized"
)

type DisruptionBudget struct {
	// Nodes is "10%" or "5"; max disruptions allowed concurrently.
	Nodes string `json:"nodes"`
	// +optional
	Schedule string `json:"schedule,omitempty"`
	// +optional
	Duration *Duration `json:"duration,omitempty"`
	// +optional
	Reasons []DisruptionReason `json:"reasons,omitempty"`
}

// Limits is an extensible ResourceList ceiling. Recognized keys:
//   cpu, memory                                standard k8s names
//   frp.operator.io/exits                      count of ExitClaims
//   frp.operator.io/bandwidthMbps              aggregate bandwidth
//   frp.operator.io/monthlyTrafficGB           aggregate traffic budget
type Limits corev1.ResourceList

type ExitPoolStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Exits int64 `json:"exits"`
	// +optional
	Resources corev1.ResourceList `json:"resources,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ExitPool{}, &ExitPoolList{})
}
