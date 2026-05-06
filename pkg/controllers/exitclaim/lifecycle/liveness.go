package lifecycle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

// AnnotationLivenessFailures records the consecutive admin-API probe
// failure count. Cleared on first success after Ready.
const AnnotationLivenessFailures = v1alpha1.Group + "/liveness-failures"

// DefaultLivenessProbeInterval is how often a Ready claim is probed.
const DefaultLivenessProbeInterval = 30 * time.Second

// DefaultLivenessFailureThreshold is the consecutive-failure count that
// flips Ready=False / Disrupted=True. Karpenter's RepairPolicies use a
// per-condition TolerationDuration; a count is simpler reasoning over
// admin-API noise here.
const DefaultLivenessFailureThreshold = 3

// Liveness implements two health-checks on an ExitClaim:
//
//  1. Pre-Ready: if Launched but never reaches Registered within
//     RegistrationTTL, mark Disrupted and delete (existing behavior).
//  2. Post-Ready (continuous): probe <PublicIP>:<AdminPort>/api/serverinfo
//     every ProbeInterval. After FailureThreshold consecutive failures
//     mark the claim Ready=False with reason AdminAPIUnreachable and
//     Disrupted=True so the disruption queue replaces it. Mirrors
//     karpenter's NodeHealth controller — periodic probe + threshold +
//     replace on persistent unreachability.
type Liveness struct {
	KubeClient   client.Client
	AdminFactory func(baseURL string) *admin.Client
	// Now is overridable in tests.
	Now func() time.Time
	// RegistrationTTL bounds the wait between Launched and Registered.
	// Zero falls back to DefaultRegistrationTTL.
	RegistrationTTL time.Duration
	// ProbeInterval controls the post-Ready probe cadence. Zero falls
	// back to DefaultLivenessProbeInterval.
	ProbeInterval time.Duration
	// FailureThreshold is the consecutive-failure count that promotes
	// the probe error to a disruption signal. Zero falls back to
	// DefaultLivenessFailureThreshold.
	FailureThreshold int
}

func (l *Liveness) ttl() time.Duration {
	if l.RegistrationTTL > 0 {
		return l.RegistrationTTL
	}
	return DefaultRegistrationTTL
}

func (l *Liveness) probeInterval() time.Duration {
	if l.ProbeInterval > 0 {
		return l.ProbeInterval
	}
	return DefaultLivenessProbeInterval
}

func (l *Liveness) threshold() int {
	if l.FailureThreshold > 0 {
		return l.FailureThreshold
	}
	return DefaultLivenessFailureThreshold
}

func (l *Liveness) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Liveness) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if !isCondTrue(claim, v1alpha1.ConditionTypeLaunched) {
		return reconcile.Result{}, nil
	}
	// Post-Ready: continuous probe.
	if isCondTrue(claim, v1alpha1.ConditionTypeReady) {
		return l.probe(ctx, claim)
	}
	// Pre-Ready: RegistrationTTL gate (existing behavior).
	if isCondTrue(claim, v1alpha1.ConditionTypeRegistered) {
		return reconcile.Result{}, nil
	}
	launchedAt := condTransitionTime(claim, v1alpha1.ConditionTypeLaunched)
	if launchedAt.IsZero() {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if l.now().Sub(launchedAt) < l.ttl() {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	orig := claim.DeepCopy()
	setCond(claim, v1alpha1.ConditionTypeDisrupted, metav1.ConditionTrue,
		v1alpha1.ReasonRegistrationTimeout, "exceeded RegistrationTTL")
	_ = l.KubeClient.Status().Patch(ctx, claim, client.MergeFrom(orig))
	if err := l.KubeClient.Delete(ctx, claim); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// probe is the post-Ready continuous health check. Returns
// RequeueAfter so the controller polls forever (no informer event
// would fire on a healthy claim otherwise).
func (l *Liveness) probe(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if claim.Status.PublicIP == "" {
		return reconcile.Result{RequeueAfter: l.probeInterval()}, nil
	}
	adminPort := claim.Spec.Frps.AdminPort
	if adminPort == 0 {
		adminPort = 7400
	}
	factory := l.AdminFactory
	if factory == nil {
		factory = admin.New
	}
	url := fmt.Sprintf("http://%s:%d", claim.Status.PublicIP, adminPort)
	logger := log.FromContext(ctx).WithValues("claim", claim.Name, "adminURL", url)

	c := factory(url)
	if _, err := c.GetServerInfo(ctx); err != nil {
		logger.V(1).Info("liveness: admin probe failed", "err", err.Error())
		return l.recordFailure(ctx, claim, err)
	}
	return l.recordSuccess(ctx, claim)
}

// recordFailure increments the consecutive-failure annotation. On
// reaching threshold it flips Ready=False + Disrupted=True so the
// disruption queue picks the claim up for replacement.
func (l *Liveness) recordFailure(
	ctx context.Context, claim *v1alpha1.ExitClaim, probeErr error,
) (reconcile.Result, error) {
	count := readFailureCount(claim) + 1
	orig := claim.DeepCopy()
	if claim.Annotations == nil {
		claim.Annotations = map[string]string{}
	}
	claim.Annotations[AnnotationLivenessFailures] = strconv.Itoa(count)
	if err := l.KubeClient.Patch(ctx, claim, client.MergeFrom(orig)); err != nil {
		return reconcile.Result{}, err
	}
	if count < l.threshold() {
		return reconcile.Result{RequeueAfter: l.probeInterval()}, nil
	}
	statusOrig := claim.DeepCopy()
	setCond(claim, v1alpha1.ConditionTypeReady, metav1.ConditionFalse,
		v1alpha1.ReasonAdminAPIUnreachable,
		fmt.Sprintf("admin probe failed %d consecutive times: %v", count, probeErr))
	setCond(claim, v1alpha1.ConditionTypeDisrupted, metav1.ConditionTrue,
		v1alpha1.ReasonAdminAPIUnreachable, "liveness threshold exceeded")
	if err := l.KubeClient.Status().Patch(ctx, claim, client.MergeFrom(statusOrig)); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: l.probeInterval()}, nil
}

// recordSuccess clears the failure counter when present. No-op when
// absent — the common case after the first probe.
func (l *Liveness) recordSuccess(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if _, ok := claim.Annotations[AnnotationLivenessFailures]; !ok {
		return reconcile.Result{RequeueAfter: l.probeInterval()}, nil
	}
	orig := claim.DeepCopy()
	delete(claim.Annotations, AnnotationLivenessFailures)
	if err := l.KubeClient.Patch(ctx, claim, client.MergeFrom(orig)); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: l.probeInterval()}, nil
}

func readFailureCount(claim *v1alpha1.ExitClaim) int {
	v := claim.Annotations[AnnotationLivenessFailures]
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
