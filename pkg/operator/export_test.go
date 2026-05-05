package operator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

// SetupIndexersForTest exposes setupIndexers to external test packages.
func SetupIndexersForTest(ctx context.Context, mgr ctrl.Manager) error {
	return setupIndexers(ctx, mgr)
}

// SetupHealthChecksForTest exposes setupHealthChecks to external test packages.
func SetupHealthChecksForTest(mgr ctrl.Manager) error {
	return setupHealthChecks(mgr)
}

