package disruption_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

const testPool = "p1"

func mkPool(budgets ...v1alpha1.DisruptionBudget) *v1alpha1.ExitPool {
	return &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: testPool},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{Budgets: budgets},
		},
	}
}

func mkClaim(name string, ready bool) *v1alpha1.ExitClaim {
	c := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{v1alpha1.LabelExitPool: testPool},
		},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: "fake://" + name,
		},
	}
	if ready {
		c.Status.Conditions = []metav1.Condition{{
			Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: "R",
			LastTransitionTime: metav1.Now(),
		}}
	}
	return c
}

// seedCluster registers N claims into a state.Cluster, marking the first
// `disrupting` of them MarkedForDeletion.
func seedCluster(n, disrupting int) *state.Cluster {
	c := state.NewCluster(nil)
	for i := 0; i < n; i++ {
		claim := mkClaim("e"+strItoa(i), true)
		c.UpdateExit(claim)
	}
	if disrupting > 0 {
		marked := 0
		for i := 0; i < n && marked < disrupting; i++ {
			c.MarkExitForDeletion("e" + strItoa(i))
			marked++
		}
	}
	return c
}

func strItoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	return out
}

func TestBudgets_DefaultTenPercent(t *testing.T) {
	c := seedCluster(20, 0)
	pool := mkPool() // no budgets
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 2 {
		t.Fatalf("want 2 (10%% of 20), got %d", got)
	}
}

func TestBudgets_DefaultFloorAtOne(t *testing.T) {
	c := seedCluster(3, 0)
	pool := mkPool()
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 1 {
		t.Fatalf("want 1 (default floor), got %d", got)
	}
}

func TestBudgets_FixedNodes(t *testing.T) {
	c := seedCluster(20, 0)
	pool := mkPool(v1alpha1.DisruptionBudget{Nodes: "5"})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestBudgets_DisruptingDeducted(t *testing.T) {
	c := seedCluster(20, 3)
	pool := mkPool(v1alpha1.DisruptionBudget{Nodes: "5"})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 2 {
		t.Fatalf("want 2 (5-3), got %d", got)
	}
}

func TestBudgets_MinAcrossBudgets(t *testing.T) {
	c := seedCluster(20, 0)
	pool := mkPool(
		v1alpha1.DisruptionBudget{Nodes: "10"},
		v1alpha1.DisruptionBudget{Nodes: "3"},
	)
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 3 {
		t.Fatalf("want 3 (min wins), got %d", got)
	}
}

func TestBudgets_ReasonFilterMatch(t *testing.T) {
	c := seedCluster(10, 0)
	pool := mkPool(v1alpha1.DisruptionBudget{
		Nodes:   "2",
		Reasons: []v1alpha1.DisruptionReason{v1alpha1.DisruptionReasonExpired},
	})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonExpired, time.Now())
	if got != 2 {
		t.Fatalf("matching reason: want 2, got %d", got)
	}
	// Non-matching reason → no budget matched → falls back to default 10%.
	got = disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 1 {
		t.Fatalf("non-matching reason → default 10%%: want 1, got %d", got)
	}
}

func TestBudgets_PercentageNodes(t *testing.T) {
	c := seedCluster(20, 0)
	pool := mkPool(v1alpha1.DisruptionBudget{Nodes: "25%"})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, time.Now())
	if got != 5 {
		t.Fatalf("want 5 (25%% of 20), got %d", got)
	}
}

func TestBudgets_CronInsideWindow(t *testing.T) {
	c := seedCluster(10, 0)
	// Schedule that fires every minute; 10s duration. now=just-after a minute boundary
	// must therefore be "inside the window".
	now, _ := time.Parse(time.RFC3339, "2026-05-04T12:00:05Z")
	pool := mkPool(v1alpha1.DisruptionBudget{
		Nodes:    "0",
		Schedule: "* * * * *",
		Duration: &v1alpha1.Duration{Duration: 30 * time.Second},
	})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, now)
	if got != 0 {
		t.Fatalf("inside cron window → budget=0: want 0, got %d", got)
	}
}

func TestBudgets_CronOutsideWindow(t *testing.T) {
	c := seedCluster(10, 0)
	// Schedule fires at the top of each hour for 5min. now is 30 min in.
	now, _ := time.Parse(time.RFC3339, "2026-05-04T12:30:00Z")
	pool := mkPool(v1alpha1.DisruptionBudget{
		Nodes:    "0",
		Schedule: "0 * * * *",
		Duration: &v1alpha1.Duration{Duration: 5 * time.Minute},
	})
	got := disruption.GetAllowedDisruptionsByReason(c, pool, v1alpha1.DisruptionReasonEmpty, now)
	// Outside the window → budget ignored → default 10% = 1.
	if got != 1 {
		t.Fatalf("outside window → default applies: want 1, got %d", got)
	}
}
