package lifecycle

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

// DefaultTerminationGracePeriod bounds how long finalize will wait for
// bound tunnels to release before forcing provider Delete.
const DefaultTerminationGracePeriod = time.Hour

// Controller orchestrates the per-claim phase chain and finalizer.
type Controller struct {
	Client        client.Client
	CloudProvider *cloudprovider.Registry
	AdminFactory  func(baseURL string) *admin.Client
	// RegistrationTTL is wired into the Liveness phase. Zero falls back
	// to DefaultRegistrationTTL.
	RegistrationTTL time.Duration

	launch         *Launcher
	registration   *Registrar
	initialization *Initializer
	liveness       *Liveness
}

// New constructs a Controller wired with all four phase reconcilers.
func New(c client.Client, cp *cloudprovider.Registry, adminFactory func(string) *admin.Client) *Controller {
	return NewWithTTL(c, cp, adminFactory, 0)
}

// NewWithTTL is like New but lets callers override the RegistrationTTL
// on the Liveness phase.
func NewWithTTL(
	c client.Client,
	cp *cloudprovider.Registry,
	adminFactory func(string) *admin.Client,
	registrationTTL time.Duration,
) *Controller {
	if adminFactory == nil {
		adminFactory = admin.New
	}
	return &Controller{
		Client:          c,
		CloudProvider:   cp,
		AdminFactory:    adminFactory,
		RegistrationTTL: registrationTTL,
		launch:          &Launcher{KubeClient: c, CloudProvider: cp},
		registration:    &Registrar{KubeClient: c, AdminFactory: adminFactory},
		initialization:  &Initializer{KubeClient: c},
		liveness:        &Liveness{KubeClient: c, RegistrationTTL: registrationTTL},
	}
}

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var claim v1alpha1.ExitClaim
	if err := r.Client.Get(ctx, req.NamespacedName, &claim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !claim.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &claim)
	}

	if controllerutil.AddFinalizer(&claim, v1alpha1.TerminationFinalizer) {
		return reconcile.Result{Requeue: true}, r.Client.Update(ctx, &claim)
	}

	for _, phase := range []func(context.Context, *v1alpha1.ExitClaim) (reconcile.Result, error){
		r.launch.Reconcile,
		r.registration.Reconcile,
		r.initialization.Reconcile,
		r.liveness.Reconcile,
	} {
		res, err := phase(ctx, &claim)
		if err != nil {
			return res, err
		}
		if !res.IsZero() {
			return res, nil
		}
	}
	return reconcile.Result{}, nil
}

// finalize drains tunnels, calls provider Delete, and strips the finalizer.
func (r *Controller) finalize(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	bound, err := r.tunnelsBoundTo(ctx, claim.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	grace := DefaultTerminationGracePeriod
	if claim.Spec.TerminationGracePeriod != nil && claim.Spec.TerminationGracePeriod.Duration > 0 {
		grace = claim.Spec.TerminationGracePeriod.Duration
	}
	deletedAt := claim.DeletionTimestamp.Time
	if len(bound) > 0 && time.Since(deletedAt) < grace {
		// Notify tunnels to release: clear their AssignedExit so the
		// scheduler reschedules them. Surface the first patch error so
		// stuck tunnels show up in events / metrics rather than silently
		// requeuing forever.
		var firstErr error
		for i := range bound {
			t := &bound[i]
			patch := client.MergeFrom(t.DeepCopy())
			t.Status.AssignedExit = ""
			t.Status.AssignedIP = ""
			t.Status.AssignedPorts = nil
			if err := r.Client.Status().Patch(ctx, t, patch); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if firstErr != nil {
			return reconcile.Result{RequeueAfter: 5 * time.Second}, firstErr
		}
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if cp, err := r.CloudProvider.For(claim.Spec.ProviderClassRef.Kind); err == nil {
		if err := cp.Delete(ctx, claim); err != nil && !cloudprovider.IsExitNotFound(err) {
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}
	} else {
		log.FromContext(ctx).V(1).Info("provider lookup failed during finalize; proceeding to strip finalizer",
			"kind", claim.Spec.ProviderClassRef.Kind, "err", err.Error())
	}

	if controllerutil.RemoveFinalizer(claim, v1alpha1.TerminationFinalizer) {
		if err := r.Client.Update(ctx, claim); err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (r *Controller) tunnelsBoundTo(ctx context.Context, claimName string) ([]v1alpha1.Tunnel, error) {
	var list v1alpha1.TunnelList
	if err := r.Client.List(ctx, &list); err != nil {
		return nil, err
	}
	out := []v1alpha1.Tunnel{}
	for i := range list.Items {
		if list.Items[i].Status.AssignedExit == claimName {
			out = append(out, list.Items[i])
		}
	}
	return out, nil
}

// SetupWithManager registers the controller.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitclaim-lifecycle").
		For(&v1alpha1.ExitClaim{}).
		Complete(r)
}
