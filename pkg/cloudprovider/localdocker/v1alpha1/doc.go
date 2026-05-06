// Package v1alpha1 contains the LocalDockerProviderClass API types.
//
// The Group (frp.operator.io) is shared with the core api/v1alpha1 package
// (which registers ExitPool, ExitClaim, Tunnel) and with sibling provider
// packages under pkg/cloudprovider/<name>/v1alpha1. Each of those packages
// owns its own SchemeBuilder; manager wiring must call this package's
// AddToScheme in addition to the core api/v1alpha1.AddToScheme. This
// package only registers the LocalDockerProviderClass kind — it does not
// re-export the core kinds.
//
// +kubebuilder:object:generate=true
// +groupName=frp.operator.io
package v1alpha1
