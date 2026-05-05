package methods_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption/methods"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func mkCandidate(name string, allocs map[int32]state.TunnelKey, pool *v1alpha1.ExitPool, lastChange time.Time) *disruption.Candidate {
	se := &state.StateExit{Allocations: allocs}
	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{v1alpha1.LabelExitPool: pool.Name}},
	}
	se.Claim = claim
	return &disruption.Candidate{
		Claim:             claim,
		State:             se,
		Pool:              pool,
		LastBindingChange: lastChange,
	}
}

func TestEmptiness_HappyPath(t *testing.T) {
	now := time.Now()
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Minute}},
		},
	}
	cand := mkCandidate("e1", nil, pool, now.Add(-2*time.Minute))
	m := &methods.Emptiness{Now: func() time.Time { return now }}
	if !m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("empty + elapsed must be disruptable")
	}
}

func TestEmptiness_NotElapsed(t *testing.T) {
	now := time.Now()
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Hour}},
		},
	}
	cand := mkCandidate("e1", nil, pool, now.Add(-1*time.Minute))
	m := &methods.Emptiness{Now: func() time.Time { return now }}
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("not elapsed must not disrupt")
	}
}

func TestEmptiness_NotEmpty(t *testing.T) {
	now := time.Now()
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Second}},
		},
	}
	allocs := map[int32]state.TunnelKey{80: "ns/t"}
	cand := mkCandidate("e1", allocs, pool, now.Add(-1*time.Hour))
	m := &methods.Emptiness{Now: func() time.Time { return now }}
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("non-empty must not disrupt")
	}
}

func TestEmptiness_AlreadyMarkedSkipped(t *testing.T) {
	now := time.Now()
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ExitPoolSpec{
			Disruption: v1alpha1.Disruption{ConsolidateAfter: v1alpha1.Duration{Duration: time.Second}},
		},
	}
	cand := mkCandidate("e1", nil, pool, now.Add(-time.Hour))
	cand.State.MarkedForDeletion = true
	m := &methods.Emptiness{Now: func() time.Time { return now }}
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("already marked must not be re-selected")
	}
}

func TestEmptiness_ComputeCommands_RespectsBudget(t *testing.T) {
	pool := &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
	cands := []*disruption.Candidate{
		mkCandidate("e1", nil, pool, time.Time{}),
		mkCandidate("e2", nil, pool, time.Time{}),
		mkCandidate("e3", nil, pool, time.Time{}),
	}
	budgets := disruption.BudgetMap{}
	budgets.Set("p", v1alpha1.DisruptionReasonEmpty, 2)
	m := methods.NewEmptiness()
	out, err := m.ComputeCommands(context.Background(), budgets, cands...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 command, got %d", len(out))
	}
	if got := len(out[0].Candidates); got != 2 {
		t.Fatalf("want 2 capped candidates, got %d", got)
	}
}

func TestEmptiness_ComputeCommands_ZeroBudgetDrops(t *testing.T) {
	pool := &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
	cands := []*disruption.Candidate{mkCandidate("e1", nil, pool, time.Time{})}
	budgets := disruption.BudgetMap{}
	m := methods.NewEmptiness()
	out, err := m.ComputeCommands(context.Background(), budgets, cands...)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("zero budget → no commands; got %d", len(out))
	}
}
