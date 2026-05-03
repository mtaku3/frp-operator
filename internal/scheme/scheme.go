/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

// Package scheme is a minimal subset of CloudNativePG's
// internal/scheme package, ported to satisfy the test surface of
// pkg/certs. Extend as further CNPG syncs require.
package scheme

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Builder contains the fluent methods to build a schema
type Builder struct {
	scheme *runtime.Scheme
}

// New creates a new builder
func New() *Builder {
	return &Builder{scheme: runtime.NewScheme()}
}

// WithClientGoScheme adds the kubernetes/scheme
func (b *Builder) WithClientGoScheme() *Builder {
	_ = clientgoscheme.AddToScheme(b.scheme)
	return b
}

// WithAPIV1Alpha1 adds the operator's v1alpha1 scheme
func (b *Builder) WithAPIV1Alpha1() *Builder {
	_ = frpv1alpha1.AddToScheme(b.scheme)
	return b
}

// WithAPIExtensionV1 adds apiextensions/v1
func (b *Builder) WithAPIExtensionV1() *Builder {
	_ = apiextensionsv1.AddToScheme(b.scheme)
	return b
}

// Build returns the built scheme
func (b *Builder) Build() *runtime.Scheme {
	return b.scheme
}

// BuildWithAllKnownScheme registers all the API used by the manager
func BuildWithAllKnownScheme() *runtime.Scheme {
	return New().
		WithAPIV1Alpha1().
		WithClientGoScheme().
		WithAPIExtensionV1().
		Build()

	// +kubebuilder:scaffold:scheme
}
