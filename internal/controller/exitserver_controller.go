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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/bootstrap"
	"github.com/mtaku3/frp-operator/internal/frp/admin"
	"github.com/mtaku3/frp-operator/internal/frp/config"
	"github.com/mtaku3/frp-operator/internal/frp/release"
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

	// Step 4: Look up Provisioner.
	if r.Provisioners == nil {
		return ctrl.Result{}, errors.New("ExitServerReconciler.Provisioners is nil — wire it in main.go")
	}
	p, err := r.Provisioners.Lookup(string(exit.Spec.Provider))
	if err != nil {
		// Treat a missing Provisioner as a permanent failure on this CR;
		// surface via condition rather than re-enqueueing forever.
		return r.patchStatusCondition(ctx, &exit, "ProviderNotRegistered", err.Error())
	}

	// Step 5: Ensure credentials Secret.
	creds, err := ensureCredentialsSecret(ctx, r.Client, &exit)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Step 5b: Render frps.toml + cloud-init user-data using Phase 2 primitives.
	frpsCfg := config.FrpsConfig{
		BindPort: portOrDefault(exit.Spec.Frps.BindPort, 7000),
		Auth:     config.FrpsAuth{Method: "token", Token: creds.AuthToken},
		WebServer: config.FrpsWebServer{
			Addr:     "0.0.0.0",
			Port:     portOrDefault(exit.Spec.Frps.AdminPort, 7500),
			User:     adminUser,
			Password: creds.AdminPassword,
		},
		AllowPorts: parseAllowPorts(exit.Spec.AllowPorts),
	}
	frpsBody, err := frpsCfg.Render()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("render frps.toml: %w", err)
	}
	cloudInit, err := bootstrap.Render(bootstrap.Input{
		FrpsConfigTOML:  frpsBody,
		BindPort:        portOrDefault(exit.Spec.Frps.BindPort, 7000),
		AdminPort:       portOrDefault(exit.Spec.Frps.AdminPort, 7500),
		AllowPortsRange: firstAllowPortsRangeString(exit.Spec.AllowPorts),
		ReservedPorts:   intsFrom32(exit.Spec.ReservedPorts),
		FrpsVersion:     release.Version,
		FrpsDownloadURL: release.DownloadURL("linux", "amd64"),
		FrpsSHA256:      release.SHA256LinuxAmd64,
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("render cloud-init: %w", err)
	}

	// Step 5c: Load provider API credentials from the referenced Secret.
	providerCreds, err := loadProviderCredentials(ctx, r.Client, &exit)
	if err != nil {
		return r.patchStatusCondition(ctx, &exit, "CredentialsUnavailable", err.Error())
	}

	// Step 6/7: Provisioner state.
	state, provErr := reconcileProvisioner(ctx, p, &exit, creds, providerCreds, cloudInit, frpsBody)
	if provErr != nil {
		// Network/transient errors get re-enqueued. ErrNotFound is treated
		// as "the resource is gone" -> Lost.
		if errors.Is(provErr, provider.ErrNotFound) {
			state.Phase = provider.PhaseGone
		} else {
			return ctrl.Result{}, provErr
		}
	}

	// Step 8: real admin probe. Build an AdminClient once we have a
	// PublicIP, the provider state is Running, and a factory injected, then
	// call ServerInfo with a short timeout. A nil error means the frps
	// webServer admin API is healthy.
	adminOK := false
	if state.PublicIP != "" && state.Phase == provider.PhaseRunning && r.NewAdminClient != nil {
		// Temporarily reflect the freshly observed PublicIP so adminBaseURL
		// can build a URL even before status is patched below.
		probeExit := exit.DeepCopy()
		probeExit.Status.PublicIP = state.PublicIP
		baseURL, urlErr := adminBaseURL(probeExit)
		if urlErr == nil {
			ac := r.NewAdminClient(baseURL, adminUser, creds.AdminPassword)
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			if _, sErr := ac.ServerInfo(probeCtx); sErr == nil {
				adminOK = true
			}
			cancel()
		}
	}

	// Step 9: Compute nextPhase and patch status.
	patch := client.MergeFrom(exit.DeepCopy())
	if state.ProviderID != "" {
		exit.Status.ProviderID = state.ProviderID
	}
	if state.PublicIP != "" {
		exit.Status.PublicIP = state.PublicIP
	}
	exit.Status.Phase = nextPhase(exit.Status.Phase, state.Phase, adminOK)
	now := metav1.Now()
	exit.Status.LastReconcileTime = &now
	if err := r.Status().Patch(ctx, &exit, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	logger.V(1).Info("reconciled ExitServer", "name", exit.Name, "phase", exit.Status.Phase, "providerID", exit.Status.ProviderID)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// patchStatusCondition writes one Condition to ExitServer.status and
// returns a non-requeueing Result (the issue is "permanent" per this call;
// resolution requires a spec change).
func (r *ExitServerReconciler) patchStatusCondition(
	ctx context.Context,
	exit *frpv1alpha1.ExitServer,
	reason, message string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(exit.DeepCopy())
	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: exit.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	apimeta.SetStatusCondition(&exit.Status.Conditions, cond)
	if err := r.Status().Patch(ctx, exit, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
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
				return ctrl.Result{}, fmt.Errorf("provisioner lookup: %w", err)
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
