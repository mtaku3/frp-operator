// Package v1alpha1 holds validating webhooks for Tunnel and ExitServer.
// All validators are pure functions of (old, new) and never call the
// Kubernetes API.
package v1alpha1

import (
	"context"
	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
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
	return ctrl.NewWebhookManagedBy(mgr, &frpv1alpha1.Tunnel{}).
		WithValidator(v).
		Complete()
}

// +kubebuilder:webhook:path=/validate-frp-operator-io-v1alpha1-tunnel,mutating=false,failurePolicy=fail,sideEffects=None,groups=frp.operator.io,resources=tunnels,verbs=create;update,versions=v1alpha1,name=vtunnel.kb.io,admissionReviewVersions=v1

// ValidateCreate implements admission.Validator. Always allows; invariants
// apply only on update.
func (v *TunnelValidator) ValidateCreate(ctx context.Context, obj *frpv1alpha1.Tunnel) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator. The lock is enforced only
// when the OLD object had Spec.ImmutableWhenReady=true AND Status.Phase=Ready.
// This means the user can flip the flag to false on an unlocked tunnel
// and immediately edit; or flip it to true on a Ready tunnel and have the
// lock take effect from the next update.
func (v *TunnelValidator) ValidateUpdate(ctx context.Context, oldT, newT *frpv1alpha1.Tunnel) (admission.Warnings, error) {
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

// ValidateDelete implements admission.Validator. Allows; finalizers handle
// teardown.
func (v *TunnelValidator) ValidateDelete(ctx context.Context, obj *frpv1alpha1.Tunnel) (admission.Warnings, error) {
	return nil, nil
}

// Compile-time check.
var _ admission.Validator[*frpv1alpha1.Tunnel] = (*TunnelValidator)(nil)
