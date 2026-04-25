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
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/admin"
	"github.com/mtaku3/frp-operator/internal/provider"
)

// AdminClient is the subset of admin.Client the controller uses. Defining
// it as an interface here lets tests inject a fake without spinning up an
// httptest server.
type AdminClient interface {
	ServerInfo(ctx context.Context) (*admin.ServerInfo, error)
	PutConfigAndReload(ctx context.Context, body []byte) error
}

// AdminClientFactory builds an AdminClient pointing at one frps's webServer.
// The default implementation in main.go wraps admin.NewClient.
type AdminClientFactory func(baseURL, user, password string) AdminClient

// ExitServerReconciler reconciles ExitServer CRs.
type ExitServerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Provisioners   *provider.Registry
	NewAdminClient AdminClientFactory
}

// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one ExitServer toward its desired state. The high-level
// state machine:
//
//  1. Fetch the CR; bail if it's gone (apiserver garbage collected).
//  2. If marked for deletion, run the finalizer (Provisioner.Destroy then
//     drop the finalizer) and exit.
//  3. Add the finalizer if missing.
//  4. Resolve the Provisioner from spec.provider.
//  5. Ensure credentials Secret exists.
//  6. If status.providerID is empty, call Provisioner.Create.
//  7. Otherwise, call Provisioner.Inspect and admin.ServerInfo for health.
//  8. Compute next phase via nextPhase(); patch status.
//  9. Requeue at the configured admin-probe interval.
//
// Tasks 4–5 fill in step 6–8. This task lands the skeleton with steps 1–3.
func (r *ExitServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var exit frpv1alpha1.ExitServer
	if err := r.Get(ctx, req.NamespacedName, &exit); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Deletion path.
	if !exit.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &exit)
	}

	// Add finalizer if missing.
	if added, err := addFinalizer(ctx, r.Client, &exit, exitServerFinalizer); err != nil {
		return ctrl.Result{}, err
	} else if added {
		// Re-fetch on next reconcile so we have the patched object.
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO(Tasks 4-5): resolve Provisioner, ensure credentials Secret, run
	// Create/Inspect, probe admin API, compute next phase, patch status.
	logger.V(1).Info("reconciling ExitServer", "name", exit.Name, "phase", exit.Status.Phase)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcileDelete tears the resource down, then drops the finalizer.
func (r *ExitServerReconciler) reconcileDelete(ctx context.Context, exit *frpv1alpha1.ExitServer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if hasFinalizer(exit, exitServerFinalizer) {
		// Find the Provisioner; if it's not registered, we still drop
		// the finalizer rather than wedging the CR forever.
		if r.Provisioners != nil && exit.Status.ProviderID != "" {
			p, err := r.Provisioners.Lookup(string(exit.Spec.Provider))
			switch {
			case err == nil:
				if destroyErr := p.Destroy(ctx, exit.Status.ProviderID); destroyErr != nil {
					return ctrl.Result{}, fmt.Errorf("provisioner Destroy: %w", destroyErr)
				}
			case errors.Is(err, provider.ErrNotRegistered):
				logger.Info("Provisioner not registered; skipping Destroy", "provider", exit.Spec.Provider)
			default:
				return ctrl.Result{}, fmt.Errorf("Provisioner lookup: %w", err)
			}
		}

		// Best-effort delete of the credentials Secret. (Owner refs would
		// also clean it up, but explicit delete is cheap insurance.)
		var sec corev1.Secret
		sec.Name = credentialsSecretName(exit)
		sec.Namespace = exit.Namespace
		if err := r.Delete(ctx, &sec); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete secret: %w", err)
		}

		if _, err := removeFinalizer(ctx, r.Client, exit, exitServerFinalizer); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager wires the controller. Watches ExitServer CRs and owned
// Secrets so the controller is notified of credential drift.
func (r *ExitServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frpv1alpha1.ExitServer{}).
		Owns(&corev1.Secret{}).
		Named("exitserver").
		Complete(r)
}
