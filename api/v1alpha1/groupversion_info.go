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

// Package v1alpha1 contains API Schema definitions for the frp v1alpha1 API group.
//
// This package registers the core kinds (ExitPool, ExitClaim, Tunnel) into
// the frp.operator.io/v1alpha1 GroupVersion. Per-provider ProviderClass
// kinds (LocalDockerProviderClass, DigitalOceanProviderClass, ...) live in
// their own packages under pkg/cloudprovider/<name>/v1alpha1 and share the
// same GroupVersion but maintain independent SchemeBuilders. Manager wiring
// must therefore call AddToScheme on each of those provider packages in
// addition to this core package — registering only this one will not
// surface ProviderClass kinds to the API server or controller cache.
//
// +kubebuilder:object:generate=true
// +groupName=frp.operator.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// SchemeGroupVersion is group version used to register these objects.
	// This name is used by applyconfiguration generators (e.g. controller-gen).
	SchemeGroupVersion = schema.GroupVersion{Group: "frp.operator.io", Version: "v1alpha1"}

	// GroupVersion is an alias for SchemeGroupVersion, for backward compatibility.
	GroupVersion = SchemeGroupVersion

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
