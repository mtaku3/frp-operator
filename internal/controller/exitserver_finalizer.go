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

package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// exitServerFinalizer is the finalizer string the controller adds to every
// ExitServer it manages. Removed only after the underlying VPS is destroyed
// (or confirmed gone) and per-exit Secrets are cleaned up.
const exitServerFinalizer = "frp.operator.io/exitserver-finalizer"

// hasFinalizer reports whether the named finalizer is on the object.
func hasFinalizer(exit *frpv1alpha1.ExitServer, name string) bool {
	for _, f := range exit.Finalizers {
		if f == name {
			return true
		}
	}
	return false
}

// addFinalizer appends the finalizer if it isn't already present and
// patches the object. Returns true if a patch was sent.
func addFinalizer(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, name string) (bool, error) {
	if hasFinalizer(exit, name) {
		return false, nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	exit.Finalizers = append(exit.Finalizers, name)
	if err := c.Patch(ctx, exit, patch); err != nil {
		return false, fmt.Errorf("add finalizer: %w", err)
	}
	return true, nil
}

// removeFinalizer drops the finalizer from the object and patches. Returns
// true if a patch was sent.
func removeFinalizer(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, name string) (bool, error) {
	if !hasFinalizer(exit, name) {
		return false, nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	out := exit.Finalizers[:0]
	for _, f := range exit.Finalizers {
		if f != name {
			out = append(out, f)
		}
	}
	exit.Finalizers = out
	if err := c.Patch(ctx, exit, patch); err != nil {
		return false, fmt.Errorf("remove finalizer: %w", err)
	}
	return true, nil
}
