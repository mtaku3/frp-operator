package informer

import (
	"context"
	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ProviderClassController is a no-op for now: it watches every
// registered ProviderClass kind so the informer cache is warm before
// dependent controllers (lifecycle, scheduler) read them. State.Cluster
// does not yet need ProviderClass entries — Phase 5+ adds caching if
// needed.
type ProviderClassController struct {
	client.Client
	Cluster *state.Cluster
	Watch   client.Object // the typed kind to watch (e.g. &ldv1alpha1.LocalDockerProviderClass{})
}

func (r *ProviderClassController) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ProviderClassController) SetupWithManager(mgr ctrl.Manager) error {
	kind := r.Watch.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		// Fallback: typed objects normally have empty TypeMeta.
		t := reflect.TypeOf(r.Watch)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		kind = t.Name()
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(fmt.Sprintf("informer-providerclass-%s", kind)).
		For(r.Watch).
		Complete(r)
}
