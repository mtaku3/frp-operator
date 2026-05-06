package operator

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption/methods"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitclaim/lifecycle"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/counter"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/hash"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/readiness"
	"github.com/mtaku3/frp-operator/pkg/controllers/exitpool/validation"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning"
	"github.com/mtaku3/frp-operator/pkg/controllers/servicewatcher"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
	"github.com/mtaku3/frp-operator/pkg/controllers/state/informer"
)

// NewScheme returns the runtime.Scheme registered for all operator types.
// Exposed as a helper so tests and tooling can build a matching scheme.
func NewScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		v1alpha1.AddToScheme,
		ldv1alpha1.AddToScheme,
		dov1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}

// Run constructs the manager and starts every operator controller. Blocks
// until ctx is cancelled or a controller returns a fatal error.
func Run(ctx context.Context, cfg *Config) error {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("kubeconfig: %w", err)
	}
	return RunWithRESTConfig(ctx, cfg, restCfg, nil)
}

// RunWithRESTConfig is the testable entrypoint. seedRegistry, if non-nil,
// is used in lieu of the built-in cloudprovider registration so tests can
// inject the in-memory fake provider.
func RunWithRESTConfig(
	ctx context.Context,
	cfg *Config,
	restCfg *rest.Config,
	seedRegistry *cloudprovider.Registry,
) error {
	logger := log.FromContext(ctx).WithName("operator")

	scheme, err := NewScheme()
	if err != nil {
		return fmt.Errorf("scheme: %w", err)
	}

	// Wire kube client throttling before NewManager so the manager's
	// shared client picks them up.
	if cfg.KubeClientQPS > 0 {
		restCfg.QPS = cfg.KubeClientQPS
	}
	if cfg.KubeClientBurst > 0 {
		restCfg.Burst = cfg.KubeClientBurst
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                        scheme,
		LeaderElection:                cfg.LeaderElection,
		LeaderElectionID:              cfg.LeaderElectionID,
		LeaderElectionNamespace:       cfg.LeaderElectionNS,
		LeaderElectionReleaseOnCancel: true,
		LeaderElectionResourceLock:    "leases",
		Metrics:                       metricsserver.Options{BindAddress: cfg.MetricsAddr},
		HealthProbeBindAddress:        cfg.HealthProbeAddr,
	})
	if err != nil {
		return fmt.Errorf("manager: %w", err)
	}

	if err := setupIndexers(ctx, mgr); err != nil {
		return fmt.Errorf("indexers: %w", err)
	}
	if err := setupHealthChecks(mgr); err != nil {
		return fmt.Errorf("health: %w", err)
	}

	cluster := state.NewCluster(mgr.GetClient())
	registry := seedRegistry
	if registry == nil {
		registry = cloudprovider.NewRegistry()
		registerBuiltinProviders(logger, mgr.GetClient(), registry)
	}

	if err := setupInformers(mgr, cluster, registry); err != nil {
		return fmt.Errorf("informers: %w", err)
	}

	prov, err := setupProvisioning(mgr, cluster, registry, cfg)
	if err != nil {
		return fmt.Errorf("provisioning: %w", err)
	}
	cluster.SetTriggers(func() { prov.Batcher.Trigger(types.UID("__cluster__")) }, nil)

	if err := setupLifecycle(mgr, cluster, registry, cfg); err != nil {
		return fmt.Errorf("lifecycle: %w", err)
	}
	if err := setupDisruption(mgr, cluster, prov, cfg); err != nil {
		return fmt.Errorf("disruption: %w", err)
	}
	if err := setupPoolControllers(mgr, registry); err != nil {
		return fmt.Errorf("pool controllers: %w", err)
	}
	if err := setupServiceWatcher(mgr); err != nil {
		return fmt.Errorf("servicewatcher: %w", err)
	}

	logger.Info("operator starting", "leaderElection", cfg.LeaderElection)
	return mgr.Start(ctx)
}

// registerBuiltinProviders attempts to construct each first-party provider
// and registers it under its ProviderClass kind. Construction failures are
// logged and skipped (e.g. Docker socket unavailable).
func registerBuiltinProviders(logger logr.Logger, kube client.Client, registry *cloudprovider.Registry) {
	if cp, err := localdocker.New(kube); err == nil {
		if err := registry.Register("LocalDockerProviderClass", cp); err != nil {
			logger.Info("localdocker register failed", "err", err.Error())
		}
	} else {
		logger.Info("localdocker provider unavailable, skipping", "err", err.Error())
	}
	if cp, err := digitalocean.New(kube, ""); err == nil {
		if err := registry.Register("DigitalOceanProviderClass", cp); err != nil {
			logger.Info("digitalocean register failed", "err", err.Error())
		}
	} else {
		logger.Info("digitalocean provider unavailable, skipping", "err", err.Error())
	}
}

// setupInformers wires the Phase-3 informer controllers + per-provider
// ProviderClass watchers. ProviderClass watchers are skipped when the
// scheme has no registration for the type — used by tests that wire the
// fake provider whose ProviderClass is a Go-only stand-in.
func setupInformers(mgr ctrl.Manager, cluster *state.Cluster, registry *cloudprovider.Registry) error {
	logger := log.Log.WithName("operator").WithName("informers")
	exitClaimCtl := &informer.ExitClaimController{Client: mgr.GetClient(), Cluster: cluster}
	if err := exitClaimCtl.SetupWithManager(mgr); err != nil {
		return err
	}
	exitPoolCtl := &informer.ExitPoolController{Client: mgr.GetClient(), Cluster: cluster}
	if err := exitPoolCtl.SetupWithManager(mgr); err != nil {
		return err
	}
	tunnelCtl := &informer.TunnelController{Client: mgr.GetClient(), Cluster: cluster}
	if err := tunnelCtl.SetupWithManager(mgr); err != nil {
		return err
	}
	scheme := mgr.GetScheme()
	for _, kind := range registry.Kinds() {
		cp, err := registry.For(kind)
		if err != nil {
			continue
		}
		for _, obj := range cp.GetSupportedProviderClasses() {
			if !scheme.Recognizes(obj.GetObjectKind().GroupVersionKind()) {
				// Try by Go type — typed objects have empty TypeMeta until populated.
				gvks, _, err := scheme.ObjectKinds(obj)
				if err != nil || len(gvks) == 0 {
					logger.Info("skipping ProviderClass watcher: type not registered with scheme",
						"kind", kindOf(obj))
					continue
				}
			}
			pcCtl := &informer.ProviderClassController{
				Client:  mgr.GetClient(),
				Cluster: cluster,
				Watch:   obj,
			}
			if err := pcCtl.SetupWithManager(mgr); err != nil {
				return err
			}
		}
	}
	return nil
}

// setupProvisioning wires the Provisioner singleton + Pod/Node controllers.
func setupProvisioning(
	mgr ctrl.Manager,
	cluster *state.Cluster,
	registry *cloudprovider.Registry,
	cfg *Config,
) (*provisioning.Provisioner, error) {
	prov := provisioning.NewWithBatcher(
		cluster, mgr.GetClient(), registry,
		cfg.BatchIdleDuration, cfg.BatchMaxDuration,
	)
	if err := prov.SetupWithManager(mgr); err != nil {
		return nil, err
	}
	podCtl := &provisioning.PodController{Client: mgr.GetClient(), Batcher: prov.Batcher}
	if err := podCtl.SetupWithManager(mgr); err != nil {
		return nil, err
	}
	nodeCtl := &provisioning.NodeController{Client: mgr.GetClient(), Batcher: prov.Batcher}
	if err := nodeCtl.SetupWithManager(mgr); err != nil {
		return nil, err
	}
	return prov, nil
}

// setupLifecycle wires the Phase-5 ExitClaim lifecycle controller.
func setupLifecycle(mgr ctrl.Manager, cluster *state.Cluster, registry *cloudprovider.Registry, cfg *Config) error {
	adminFactory := func(baseURL string) *admin.Client { return admin.New(baseURL) }
	c := lifecycle.NewWithTTL(mgr.GetClient(), registry, adminFactory, cfg.RegistrationTTL)
	c.Cluster = cluster
	return c.SetupWithManager(mgr)
}

// provisionerAdapter adapts the provisioning.Provisioner to the disruption
// queue's ProvisionerTrigger contract. The provisioner currently handles
// claim creation through its own scheduler loop; for disruption-driven
// replacements we fall back to direct Create calls.
type provisionerAdapter struct {
	client client.Client
	prov   *provisioning.Provisioner
}

func (a *provisionerAdapter) CreateReplacements(ctx context.Context, claims []*v1alpha1.ExitClaim) error {
	for _, c := range claims {
		if err := a.client.Create(ctx, c); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	a.prov.Batcher.Trigger(types.UID("__disruption__"))
	return nil
}

// setupDisruption wires the Phase-6 disruption controller + queue.
func setupDisruption(mgr ctrl.Manager, cluster *state.Cluster, prov *provisioning.Provisioner, cfg *Config) error {
	queue := &disruption.Queue{
		Client:                  mgr.GetClient(),
		Cluster:                 cluster,
		Provisioner:             &provisionerAdapter{client: mgr.GetClient(), prov: prov},
		ReplacementReadyTimeout: disruption.DefaultReplacementReadyTimeout,
		ReplacementPollInterval: disruption.DefaultReplacementPollInterval,
	}
	dc := disruption.New(cluster, mgr.GetClient(), queue, methods.DefaultMethods(cluster, mgr.GetClient()))
	if cfg.DisruptionPollPeriod > 0 {
		dc.PollInterval = cfg.DisruptionPollPeriod
	}
	return dc.SetupWithManager(mgr)
}

// setupPoolControllers wires the Phase-7 ExitPool ancillary controllers.
func setupPoolControllers(mgr ctrl.Manager, registry *cloudprovider.Registry) error {
	if err := (&hash.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&counter.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		return err
	}
	readinessCtl := &readiness.Controller{
		Client:       mgr.GetClient(),
		KindToObject: providerClassFactories(registry, mgr.GetScheme()),
	}
	if err := readinessCtl.SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&validation.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		return err
	}
	return nil
}

// setupServiceWatcher wires the Phase-8 Service↔Tunnel translation pair.
func setupServiceWatcher(mgr ctrl.Manager) error {
	if err := (&servicewatcher.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		return err
	}
	return (&servicewatcher.ReverseSync{Client: mgr.GetClient()}).SetupWithManager(mgr)
}

// providerClassFactories builds the KindToObject map consumed by
// readiness.Controller. Keys are GVK kind names ("LocalDockerProviderClass").
// Resolution prefers scheme.ObjectKinds — the authoritative source — and
// falls back to reflection only when the scheme has no registration (tests
// that wire a fake provider whose ProviderClass is a Go-only stand-in).
func providerClassFactories(registry *cloudprovider.Registry, scheme *runtime.Scheme) map[string]func() client.Object {
	out := map[string]func() client.Object{}
	for _, kind := range registry.Kinds() {
		cp, err := registry.For(kind)
		if err != nil {
			continue
		}
		for _, obj := range cp.GetSupportedProviderClasses() {
			name := ""
			if scheme != nil {
				if gvks, _, err := scheme.ObjectKinds(obj); err == nil && len(gvks) > 0 {
					name = gvks[0].Kind
				}
			}
			if name == "" {
				name = kindOf(obj)
			}
			if name == "" {
				continue
			}
			template := obj
			out[name] = func() client.Object {
				return template.DeepCopyObject().(client.Object)
			}
		}
	}
	return out
}

// kindOf extracts the Go type name as a stand-in for GVK Kind. Typed
// objects from generated clients have empty TypeMeta, so reflection is
// the most reliable identifier.
func kindOf(obj client.Object) string {
	if k := obj.GetObjectKind().GroupVersionKind().Kind; k != "" {
		return k
	}
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
