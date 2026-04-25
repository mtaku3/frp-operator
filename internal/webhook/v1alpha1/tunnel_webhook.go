// Package v1alpha1 holds validating webhooks for Tunnel and ExitServer.
// All validators are pure functions of (old, new) and never call the
// Kubernetes API.
package v1alpha1

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// TunnelValidator enforces Spec.ImmutableWhenReady semantics: once the
// tunnel is Ready and locked, rebind-triggering fields are immutable
// until the user explicitly unlocks (sets ImmutableWhenReady=false).
//
// Locked rebind-triggering fields:
//   - Spec.ExitRef
//   - Spec.Ports (any change: add/remove/edit)
//   - Spec.Service
type TunnelValidator struct{}

// SetupWithManager wires this validator into the manager.
func (v *TunnelValidator) SetupWithManager(mgr ctrl.Manager) error {
	// TODO: Wire the webhook with the manager when running on a cluster
	return nil
}

// +kubebuilder:webhook:path=/validate-frp-operator-io-v1alpha1-tunnel,mutating=false,failurePolicy=fail,sideEffects=None,groups=frp.operator.io,resources=tunnels,verbs=create;update,versions=v1alpha1,name=vtunnel.kb.io,admissionReviewVersions=v1

// ValidateCreate implements webhook.CustomValidator. Always allows;
// invariants apply only on update.
func (v *TunnelValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	if _, ok := obj.(*frpv1alpha1.Tunnel); !ok {
		return nil, fmt.Errorf("expected Tunnel, got %T", obj)
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator. The lock is enforced
// only when the OLD object had Spec.ImmutableWhenReady=true AND
// Status.Phase=Ready. This means the user can flip the flag to false on
// an unlocked tunnel and immediately edit; or flip it to true on a Ready
// tunnel and have the lock take effect from the next update.
func (v *TunnelValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldT, ok := oldObj.(*frpv1alpha1.Tunnel)
	if !ok {
		return nil, fmt.Errorf("expected old Tunnel, got %T", oldObj)
	}
	newT, ok := newObj.(*frpv1alpha1.Tunnel)
	if !ok {
		return nil, fmt.Errorf("expected new Tunnel, got %T", newObj)
	}

	// Lock applies only when old.Spec.ImmutableWhenReady was true AND
	// old.Status.Phase was Ready. If the user just toggled the flag or
	// the tunnel isn't Ready yet, nothing is locked.
	if !oldT.Spec.ImmutableWhenReady || oldT.Status.Phase != frpv1alpha1.TunnelReady {
		return nil, nil
	}
	// If new.Spec.ImmutableWhenReady is false, that's the unlock event;
	// permit it unconditionally so users can recover.
	if !newT.Spec.ImmutableWhenReady {
		return nil, nil
	}

	// Locked: check that rebind-triggering fields didn't change.
	if !reflect.DeepEqual(oldT.Spec.ExitRef, newT.Spec.ExitRef) {
		return nil, fmt.Errorf("spec.exitRef is immutable while immutableWhenReady=true and phase=Ready (clear immutableWhenReady first)")
	}
	if !reflect.DeepEqual(oldT.Spec.Ports, newT.Spec.Ports) {
		return nil, fmt.Errorf("spec.ports is immutable while immutableWhenReady=true and phase=Ready")
	}
	if !reflect.DeepEqual(oldT.Spec.Service, newT.Spec.Service) {
		return nil, fmt.Errorf("spec.service is immutable while immutableWhenReady=true and phase=Ready")
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator. Allows; finalizers
// handle teardown.
func (v *TunnelValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// Compile-time check.
var _ webhook.CustomValidator = (*TunnelValidator)(nil)
