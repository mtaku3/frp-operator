package disruption

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// DefaultBudgetPercent is the implicit budget when a pool declares no budgets.
// Mirrors Karpenter's "10%" default.
const DefaultBudgetPercent = 10

// GetAllowedDisruptionsByReason returns the per-pool, per-reason remaining
// budget. The minimum across all matching active budgets wins, then ongoing
// disruptions (StateExit.MarkedForDeletion) are deducted.
//
// Reason match: a budget without Reasons applies to every reason; a budget with
// Reasons applies only to those reasons.
//
// Schedule: empty schedule = always active. Otherwise the budget is active in
// the [Next-1, Next-1+Duration) windows produced by ParseStandard. We compare
// "now" against the most recent past-or-current window.
func GetAllowedDisruptionsByReason(
	c *state.Cluster,
	pool *v1alpha1.ExitPool,
	reason v1alpha1.DisruptionReason,
	now time.Time,
) int {
	if pool == nil {
		return 0
	}
	nodesInPool := countExitsInPool(c, pool.Name)
	minBudget := math.MaxInt32
	matched := false
	for _, b := range pool.Spec.Disruption.Budgets {
		if !budgetActive(b, reason, now) {
			continue
		}
		matched = true
		cap := resolveBudgetCount(b.Nodes, nodesInPool)
		if cap < minBudget {
			minBudget = cap
		}
	}
	if !matched {
		// Default: 10% of pool size, rounded down, floored at 1.
		minBudget = nodesInPool * DefaultBudgetPercent / 100
		if minBudget < 1 {
			minBudget = 1
		}
	}
	disrupting := countDisruptingInPool(c, pool.Name)
	allowed := minBudget - disrupting
	if allowed < 0 {
		allowed = 0
	}
	return allowed
}

func budgetActive(b v1alpha1.DisruptionBudget, reason v1alpha1.DisruptionReason, now time.Time) bool {
	if len(b.Reasons) > 0 {
		match := false
		for _, r := range b.Reasons {
			if r == reason {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if b.Schedule == "" {
		return true
	}
	dur := time.Duration(0)
	if b.Duration != nil {
		dur = b.Duration.Duration
	}
	return cronActive(b.Schedule, dur, now)
}

// cronActive returns true if `now` falls inside the most recent
// [trigger, trigger+duration) window of the cron schedule. A zero or negative
// duration disables the budget when a schedule is set (matches karpenter
// validation: schedule + duration must be co-specified).
//
// We use the Karpenter trick: ask for the Next fire strictly after
// (now - duration). If that fire is <= now, then `now` is inside the
// [fire, fire+duration) window. This requires fires to be spaced wider than
// `duration` (a sensible budget config).
func cronActive(schedule string, duration time.Duration, now time.Time) bool {
	if duration <= 0 {
		return false
	}
	sched, err := cron.ParseStandard(schedule)
	if err != nil {
		return false
	}
	fire := sched.Next(now.Add(-duration))
	return !fire.After(now)
}

func resolveBudgetCount(nodes string, total int) int {
	nodes = strings.TrimSpace(nodes)
	if strings.HasSuffix(nodes, "%") {
		pct, err := strconv.Atoi(strings.TrimSuffix(nodes, "%"))
		if err != nil || pct < 0 {
			return 0
		}
		return (total * pct) / 100
	}
	n, err := strconv.Atoi(nodes)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func countExitsInPool(c *state.Cluster, name string) int {
	count := 0
	for _, se := range c.Exits() {
		claim, _ := se.SnapshotForRead()
		if claim == nil {
			continue
		}
		if claim.Labels[v1alpha1.LabelExitPool] == name {
			count++
		}
	}
	return count
}

func countDisruptingInPool(c *state.Cluster, name string) int {
	count := 0
	for _, se := range c.Exits() {
		claim, _ := se.SnapshotForRead()
		if claim == nil {
			continue
		}
		if claim.Labels[v1alpha1.LabelExitPool] != name {
			continue
		}
		if se.IsMarkedForDeletion() {
			count++
		}
	}
	return count
}
