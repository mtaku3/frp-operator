package operator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Field index keys. Exposed as constants so callers can use them with
// client.MatchingFields without re-declaring the literal strings.
const (
	IndexExitClaimProviderID       = "status.providerID"
	IndexExitClaimProviderClassRef = "spec.providerClassRef.name"
	IndexTunnelAssignedExit        = "status.assignedExit"
)

// setupIndexers registers the field indexers required by the controllers
// (per spec §10).
func setupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, IndexExitClaimProviderID,
		func(o client.Object) []string {
			c := o.(*v1alpha1.ExitClaim)
			if c.Status.ProviderID == "" {
				return nil
			}
			return []string{c.Status.ProviderID}
		}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, IndexExitClaimProviderClassRef,
		func(o client.Object) []string {
			c := o.(*v1alpha1.ExitClaim)
			if c.Spec.ProviderClassRef.Name == "" {
				return nil
			}
			return []string{c.Spec.ProviderClassRef.Name}
		}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Tunnel{}, IndexTunnelAssignedExit,
		func(o client.Object) []string {
			t := o.(*v1alpha1.Tunnel)
			if t.Status.AssignedExit == "" {
				return nil
			}
			return []string{t.Status.AssignedExit}
		}); err != nil {
		return err
	}
	return nil
}
