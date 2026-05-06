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

func driftCand(claimHash, poolHash string) *disruption.Candidate {
	pool := &v1alpha1.ExitPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p",
			Annotations: map[string]string{v1alpha1.AnnotationPoolHash: poolHash},
		},
	}
	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "e",
			Labels:      map[string]string{v1alpha1.LabelExitPool: "p"},
			Annotations: map[string]string{v1alpha1.AnnotationPoolHash: claimHash},
		},
	}
	se := &state.StateExit{Claim: claim}
	return &disruption.Candidate{Claim: claim, State: se, Pool: pool, LastBindingChange: time.Now()}
}

func TestDrift_HashMismatch(t *testing.T) {
	m := methods.NewDrift()
	if !m.ShouldDisrupt(context.Background(), driftCand("a", "b")) {
		t.Fatal("hash mismatch should fire drift")
	}
}

func TestDrift_HashMatch(t *testing.T) {
	m := methods.NewDrift()
	if m.ShouldDisrupt(context.Background(), driftCand("x", "x")) {
		t.Fatal("matching hashes should not fire drift")
	}
}

func TestDrift_AnnotationsMissingSafe(t *testing.T) {
	m := methods.NewDrift()
	if m.ShouldDisrupt(context.Background(), driftCand("", "")) {
		t.Fatal("missing annotations must not trigger drift")
	}
	if m.ShouldDisrupt(context.Background(), driftCand("a", "")) {
		t.Fatal("missing pool annotation must not trigger drift")
	}
}

func TestDrift_ComputeCommands_HasReplacements(t *testing.T) {
	cand := driftCand("a", "b")
	budgets := disruption.BudgetMap{}
	budgets.Set("p", v1alpha1.DisruptionReasonDrifted, 5)
	cmds, err := methods.NewDrift().ComputeCommands(context.Background(), budgets, cand)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || len(cmds[0].Replacements) != 1 {
		t.Fatalf("want 1 cmd with 1 replacement, got %#v", cmds)
	}
	if cmds[0].Replacements[0].Annotations[v1alpha1.AnnotationPoolHash] != "b" {
		t.Fatal("replacement should carry the pool's current hash")
	}
}
