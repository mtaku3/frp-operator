// Package cloudprovider defines the contract that every cloud-side
// provisioner must implement. Mirrors sigs.k8s.io/karpenter
// pkg/cloudprovider/types.go. Implementations live in sub-packages
// (fake/, localdocker/, digitalocean/, ...).
package cloudprovider
