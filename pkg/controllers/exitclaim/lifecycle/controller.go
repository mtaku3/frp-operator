package lifecycle

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	launch         *Launcher
	registration   *Registrar
	initialization *Initializer
	liveness       *Liveness
}

// New constructs a Controller wired with all four phase reconcilers.
func New(c client.Client, cp *cloudprovider.Registry, adminFactory func(string) *admin.Client) *Controller {
	if adminFactory == nil {
		adminFactory = admin.New
	}
	return &Controller{
		Client:         c,
		CloudProvider:  cp,
		AdminFactory:   adminFactory,
		launch:         &Launcher{KubeClient: c, CloudProvider: cp},
		registration:   &Registrar{KubeClient: c, AdminFactory: adminFactory},
		initialization: &Initializer{KubeClient: c},
		liveness:       &Liveness{KubeClient: c},
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
		// scheduler reschedules them.
		for i := range bound {
			t := &bound[i]
			patch := client.MergeFrom(t.DeepCopy())
			t.Status.AssignedExit = ""
			t.Status.AssignedIP = ""
			t.Status.AssignedPorts = nil
			_ = r.Client.Status().Patch(ctx, t, patch)
		}
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if cp, err := r.CloudProvider.For(claim.Spec.ProviderClassRef.Kind); err == nil {
		if err := cp.Delete(ctx, claim); err != nil && !cloudprovider.IsExitNotFound(err) {
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}
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
