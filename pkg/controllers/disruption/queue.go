package disruption

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ProvisionerTrigger is the slim subset of the Phase 4 provisioner that the
// disruption queue needs: a way to ask for replacement claims to be created.
// Phase 9 wiring adapts the concrete provisioner (or a Create loop) to this
// interface.
type ProvisionerTrigger interface {
	CreateReplacements(ctx context.Context, claims []*v1alpha1.ExitClaim) error
}

// DefaultReplacementReadyTimeout caps how long the queue waits for a
// replacement claim to reach Ready before giving up on the command.
const DefaultReplacementReadyTimeout = 5 * time.Minute

// DefaultReplacementPollInterval governs the poll cadence while waiting.
const DefaultReplacementPollInterval = 5 * time.Second

// Queue executes Commands. It is safe to share across goroutines as long as
// callers serialize Enqueue invocations (the disruption controller is the
// only caller).
type Queue struct {
	Client                  client.Client
	Cluster                 *state.Cluster
	Provisioner             ProvisionerTrigger
	ReplacementReadyTimeout time.Duration
	ReplacementPollInterval time.Duration
	Now                     func() time.Time
}

// NewQueue constructs a Queue with default timing.
func NewQueue(c client.Client, cluster *state.Cluster, p ProvisionerTrigger) *Queue {
	return &Queue{
		Client:                  c,
		Cluster:                 cluster,
		Provisioner:             p,
		ReplacementReadyTimeout: DefaultReplacementReadyTimeout,
		ReplacementPollInterval: DefaultReplacementPollInterval,
		Now:                     time.Now,
	}
}

// Enqueue executes the command synchronously:
//
//  1. Cordon every candidate by stamping Disrupted=True on its claim.
//     Survives operator restart, unlike the in-memory MarkExitForDeletion
//     gate. Karpenter analog: applying the karpenter.sh/disruption taint
//     before launching replacements.
//  2. Launch replacements via the provisioner adapter. The cordon ensures
//     the scheduler stops binding new tunnels onto candidates during the
//     replacement-Ready wait window, which can be many minutes.
//  3. Wait for replacements to reach Ready.
//  4. Mark candidates in-memory and trigger the API delete. The lifecycle
//     finalizer takes over from there to drain tunnels + call
//     cloudProvider.Delete.
func (q *Queue) Enqueue(ctx context.Context, cmd *Command) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}
	logger := log.FromContext(ctx).WithValues("method", cmd.Method, "reason", cmd.Reason)

	// 1. Cordon candidates first so the scheduler stops binding new
	// tunnels during the (potentially long) replacement-Ready wait.
	for _, cand := range cmd.Candidates {
		if cand == nil || cand.Claim == nil {
			continue
		}
		if err := q.cordon(ctx, cand.Claim.Name, cmd.Reason); err != nil {
			return fmt.Errorf("cordon claim %s: %w", cand.Claim.Name, err)
		}
	}

	// 2. Launch replacements. Cordon already gates the scheduler, so a
	// wait timeout won't leave new tunnels piled onto a doomed exit.
	if len(cmd.Replacements) > 0 {
		if q.Provisioner == nil {
			return fmt.Errorf("command requires replacements but no ProvisionerTrigger wired")
		}
		if err := q.Provisioner.CreateReplacements(ctx, cmd.Replacements); err != nil {
			return fmt.Errorf("create replacements: %w", err)
		}
		if err := q.waitForReplacementsReady(ctx, cmd.Replacements); err != nil {
			return fmt.Errorf("wait for replacements: %w", err)
		}
	}

	// 3. Mark candidates and trigger the API delete. The mark gates the
	// provisioner's in-memory cache; the lifecycle finalizer drains.
	for _, cand := range cmd.Candidates {
		if cand == nil || cand.Claim == nil {
			continue
		}
		q.Cluster.MarkExitForDeletion(cand.Claim.Name)
		var live v1alpha1.ExitClaim
		if err := q.Client.Get(ctx, types.NamespacedName{Name: cand.Claim.Name}, &live); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("get claim %s: %w", cand.Claim.Name, err)
		}
		if live.DeletionTimestamp != nil {
			continue
		}
		if err := q.Client.Delete(ctx, &live); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete claim %s: %w", cand.Claim.Name, err)
		}
		logger.Info("disruption deleted exit", "exitClaim", cand.Claim.Name)
	}
	return nil
}

// cordon stamps Disrupted=True on the named claim so the scheduler
// excludes it from new bindings. Idempotent on conflict (single retry
// after a fresh Get). The condition's Reason carries the disruption
// reason for observability.
func (q *Queue) cordon(ctx context.Context, claimName string, reason v1alpha1.DisruptionReason) error {
	for range 2 {
		var live v1alpha1.ExitClaim
		if err := q.Client.Get(ctx, types.NamespacedName{Name: claimName}, &live); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if live.DeletionTimestamp != nil {
			return nil
		}
		if disruptedTrue(live.Status.Conditions) {
			return nil
		}
		patch := client.MergeFrom(live.DeepCopy())
		live.Status.Conditions = upsertCondition(live.Status.Conditions, metav1.Condition{
			Type:               v1alpha1.ConditionTypeDisrupted,
			Status:             metav1.ConditionTrue,
			Reason:             string(reason),
			Message:            fmt.Sprintf("disruption: %s", reason),
			LastTransitionTime: metav1.Now(),
		})
		err := q.Client.Status().Patch(ctx, &live, patch)
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			return err
		}
	}
	return fmt.Errorf("cordon claim %s: conflict after retry", claimName)
}

func disruptedTrue(conds []metav1.Condition) bool {
	for _, c := range conds {
		if c.Type == v1alpha1.ConditionTypeDisrupted && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func upsertCondition(conds []metav1.Condition, in metav1.Condition) []metav1.Condition {
	for i := range conds {
		if conds[i].Type == in.Type {
			if conds[i].Status == in.Status {
				in.LastTransitionTime = conds[i].LastTransitionTime
			}
			conds[i] = in
			return conds
		}
	}
	return append(conds, in)
}

// waitForReplacementsReady polls until every replacement claim — looked up by
// name — reaches Ready=True, or the timeout fires.
func (q *Queue) waitForReplacementsReady(ctx context.Context, replacements []*v1alpha1.ExitClaim) error {
	if len(replacements) == 0 {
		return nil
	}
	deadline := q.now().Add(q.ReplacementReadyTimeout)
	for {
		allReady := true
		for _, r := range replacements {
			if r == nil || r.Name == "" {
				// Names are populated post-create; if a caller passed an
				// unnamed claim, skip — we have no way to look it up.
				continue
			}
			var live v1alpha1.ExitClaim
			if err := q.Client.Get(ctx, types.NamespacedName{Name: r.Name}, &live); err != nil {
				if apierrors.IsNotFound(err) {
					allReady = false
					break
				}
				return err
			}
			if !readyTrue(live.Status.Conditions) {
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}
		if !q.now().Before(deadline) {
			return fmt.Errorf("replacements did not become Ready within %s", q.ReplacementReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(q.ReplacementPollInterval):
		}
	}
}

func readyTrue(conds []metav1.Condition) bool {
	for _, c := range conds {
		if c.Type == v1alpha1.ConditionTypeReady && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}
