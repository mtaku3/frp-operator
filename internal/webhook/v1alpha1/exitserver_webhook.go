package v1alpha1

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ExitServerValidator enforces grow-only semantics on Spec.AllowPorts.
// An update may add ranges; it may not remove a range that's still
// covered by Status.Allocations.
type ExitServerValidator struct{}

// SetupWithManager wires this validator into the manager.
func (v *ExitServerValidator) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &frpv1alpha1.ExitServer{}).
		WithValidator(v).
		Complete()
}

// +kubebuilder:webhook:path=/validate-frp-operator-io-v1alpha1-exitserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=frp.operator.io,resources=exitservers,verbs=create;update,versions=v1alpha1,name=vexitserver.kb.io,admissionReviewVersions=v1

// ValidateCreate implements admission.Validator.
func (v *ExitServerValidator) ValidateCreate(ctx context.Context, obj *frpv1alpha1.ExitServer) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator. AllowPorts must cover every
// port currently allocated.
func (v *ExitServerValidator) ValidateUpdate(ctx context.Context, oldE, newE *frpv1alpha1.ExitServer) (admission.Warnings, error) {
	ranges, err := parseAllowPorts(newE.Spec.AllowPorts)
	if err != nil {
		return nil, fmt.Errorf("spec.allowPorts: %w", err)
	}
	for portStr := range newE.Status.Allocations {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			// Status carries arbitrary content; ignore unparseable keys.
			continue
		}
		if !portCovered(p, ranges) {
			return nil, fmt.Errorf("spec.allowPorts no longer covers allocated port %d (used by %s)",
				p, newE.Status.Allocations[portStr])
		}
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator.
func (v *ExitServerValidator) ValidateDelete(ctx context.Context, obj *frpv1alpha1.ExitServer) (admission.Warnings, error) {
	return nil, nil
}

// portRange is one inclusive range of ports.
type portRange struct{ Start, End int }

// parseAllowPorts parses entries like "443" or "1024-65535" into ranges.
// Single-port entries become Range{p, p}.
func parseAllowPorts(specs []string) ([]portRange, error) {
	out := make([]portRange, 0, len(specs))
	for _, s := range specs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if before, after, ok := strings.Cut(s, "-"); ok {
			start, err1 := strconv.Atoi(before)
			end, err2 := strconv.Atoi(after)
			if err1 != nil || err2 != nil || start > end || start < 1 || end > 65535 {
				return nil, fmt.Errorf("invalid range %q", s)
			}
			out = append(out, portRange{start, end})
			continue
		}
		p, err := strconv.Atoi(s)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid port %q", s)
		}
		out = append(out, portRange{p, p})
	}
	return out, nil
}

// portCovered returns true if any range contains p.
func portCovered(p int, ranges []portRange) bool {
	for _, r := range ranges {
		if p >= r.Start && p <= r.End {
			return true
		}
	}
	return false
}

var _ admission.Validator[*frpv1alpha1.ExitServer] = (*ExitServerValidator)(nil)
