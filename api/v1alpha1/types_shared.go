/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License").
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Duration is a wrapper for metav1.Duration so callers don't need to import
// the metav1 package just for one field.
type Duration = metav1.Duration

// LocalObjectReference is a reference to a resource in the same namespace.
type LocalObjectReference struct {
	Name string `json:"name"`
}

// SecretKeyRef points at a key inside a Secret in the same namespace as the
// referencing object.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ProviderClassRef is a typed pointer to a per-provider config CRD.
// Mirrors Karpenter NodeClassReference{Group, Kind, Name}.
type ProviderClassRef struct {
	// Group is the API group of the ProviderClass kind, e.g. frp.operator.io.
	Group string `json:"group"`
	// Kind is the ProviderClass kind, e.g. LocalDockerProviderClass.
	Kind string `json:"kind"`
	// Name is the metadata.name of the ProviderClass instance.
	Name string `json:"name"`
}

// NodeSelectorOperator mirrors corev1.NodeSelectorOperator with explicit
// support for Karpenter-style operators (Gt, Lt) included.
type NodeSelectorOperator string

const (
	NodeSelectorOpIn           NodeSelectorOperator = "In"
	NodeSelectorOpNotIn        NodeSelectorOperator = "NotIn"
	NodeSelectorOpExists       NodeSelectorOperator = "Exists"
	NodeSelectorOpDoesNotExist NodeSelectorOperator = "DoesNotExist"
	NodeSelectorOpGt           NodeSelectorOperator = "Gt"
	NodeSelectorOpLt           NodeSelectorOperator = "Lt"
)

// NodeSelectorRequirementWithMinValues is the requirement struct used in
// ExitPool/ExitClaim/Tunnel requirements lists. It mirrors Karpenter's
// equivalent and adds the optional MinValues knob.
type NodeSelectorRequirementWithMinValues struct {
	// Key is the label/requirement key.
	Key string `json:"key"`
	// Operator is one of In, NotIn, Exists, DoesNotExist, Gt, Lt.
	Operator NodeSelectorOperator `json:"operator"`
	// Values is the operand list. Required for In, NotIn, Gt, Lt;
	// must be empty for Exists, DoesNotExist.
	// +optional
	Values []string `json:"values,omitempty"`
	// MinValues forces the scheduler to consider at least N distinct
	// values for this key when packing.
	// +optional
	MinValues *int `json:"minValues,omitempty"`
}

// ResourceRequirements is an extensible ResourceList wrapper. Same shape
// as Karpenter's NodeClaim.Spec.Resources.Requests and core
// Pod.Spec.Containers[].Resources.Requests. Recognized keys are
// documented in the spec.
type ResourceRequirements struct {
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// FrpsConfig is the full set of frps daemon options carried on
// ExitPool template + ExitClaim spec. Defaults applied by the
// scheduler/lifecycle controller when fields are omitted.
type FrpsConfig struct {
	// Version is the frps binary version, e.g. "v0.68.1". Drives image
	// tag (localdocker) or binary URL (DO cloud-init).
	Version string `json:"version"`
	// BindPort is the control-plane TCP port frpc connects to.
	// +kubebuilder:default=7000
	BindPort int32 `json:"bindPort,omitempty"`
	// AdminPort is the frps admin HTTP API port.
	// +kubebuilder:default=7400
	AdminPort int32 `json:"adminPort,omitempty"`
	// VhostHTTPPort is the optional HTTP vhost listener.
	// +optional
	VhostHTTPPort *int32 `json:"vhostHTTPPort,omitempty"`
	// VhostHTTPSPort is the optional HTTPS vhost listener.
	// +optional
	VhostHTTPSPort *int32 `json:"vhostHTTPSPort,omitempty"`
	// KCPBindPort is the optional KCP transport listener.
	// +optional
	KCPBindPort *int32 `json:"kcpBindPort,omitempty"`
	// QUICBindPort is the optional QUIC transport listener.
	// +optional
	QUICBindPort *int32 `json:"quicBindPort,omitempty"`
	// AllowPorts is the set of public port slots, e.g.
	// ["80","443","1024-65535"]. Scheduler binpacks port-conflicts here.
	AllowPorts []string `json:"allowPorts"`
	// ReservedPorts are subtracted from AllowPorts; frps internal/admin
	// ports auto-merged in by the scheduler.
	// +optional
	ReservedPorts []int32 `json:"reservedPorts,omitempty"`
	// Auth declares how frpc clients authenticate to frps.
	Auth FrpsAuthConfig `json:"auth"`
	// TLS pins the TLS material for the control plane.
	// +optional
	TLS *FrpsTLSConfig `json:"tls,omitempty"`
}

// FrpsAuthConfig declares the auth mode. v1 supports only token.
type FrpsAuthConfig struct {
	// Method selects the auth mode. Currently only "token" is implemented.
	// +kubebuilder:validation:Enum=token
	Method string `json:"method"`
	// TokenSecretRef references a Secret holding the shared token under
	// the named key. If unset, the operator generates a token at
	// provision time and writes a managed Secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// FrpsTLSConfig is the TLS material for the control plane.
type FrpsTLSConfig struct {
	// Force requires TLS for incoming frpc connections.
	// +optional
	Force bool `json:"force,omitempty"`
	// CertSecret references the server certificate.
	// +optional
	CertSecret *SecretKeyRef `json:"certSecret,omitempty"`
	// KeySecret references the server private key.
	// +optional
	KeySecret *SecretKeyRef `json:"keySecret,omitempty"`
	// CASecret enables mTLS by requiring clients to present a cert
	// signed by this CA.
	// +optional
	CASecret *SecretKeyRef `json:"caSecret,omitempty"`
}
