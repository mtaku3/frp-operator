package operator

import (
	"context"
	"fmt"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// setupHealthChecks registers /healthz and /readyz on the manager. Readyz
// includes a CRD-presence check that lists each operator-owned CRD with
// limit=1. If any List fails, readyz fails.
func setupHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("crd", crdReadinessCheck(mgr.GetClient())); err != nil {
		return err
	}
	return nil
}

// crdReadinessCheck returns a healthz.Checker that lists each operator
// CRD (limit=1) to confirm the CRDs are installed and the apiserver is
// reachable.
func crdReadinessCheck(c client.Client) healthz.Checker {
	return func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		if err := c.List(ctx, &v1alpha1.ExitPoolList{}, client.Limit(1)); err != nil {
			return fmt.Errorf("ExitPool CRD not ready: %w", err)
		}
		if err := c.List(ctx, &v1alpha1.ExitClaimList{}, client.Limit(1)); err != nil {
			return fmt.Errorf("ExitClaim CRD not ready: %w", err)
		}
		if err := c.List(ctx, &v1alpha1.TunnelList{}, client.Limit(1)); err != nil {
			return fmt.Errorf("tunnel CRD not ready: %w", err)
		}
		return nil
	}
}
