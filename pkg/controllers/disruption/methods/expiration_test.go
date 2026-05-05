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

func expirationCand(name string, age time.Duration, expireAfter time.Duration) *disruption.Candidate {
	pool := &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Labels:            map[string]string{v1alpha1.LabelExitPool: "p"},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
		},
		Spec: v1alpha1.ExitClaimSpec{ExpireAfter: v1alpha1.Duration{Duration: expireAfter}},
	}
	se := &state.StateExit{Claim: claim}
	return &disruption.Candidate{Claim: claim, State: se, Pool: pool}
}

func TestExpiration_Fires(t *testing.T) {
	m := methods.NewExpiration()
	cand := expirationCand("e", 2*time.Hour, time.Hour)
	if !m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("age > expireAfter must fire")
	}
}

func TestExpiration_NotElapsed(t *testing.T) {
	m := methods.NewExpiration()
	cand := expirationCand("e", time.Minute, time.Hour)
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("not elapsed must not fire")
	}
}

func TestExpiration_NoExpireAfter(t *testing.T) {
	m := methods.NewExpiration()
	cand := expirationCand("e", 24*time.Hour, 0)
	if m.ShouldDisrupt(context.Background(), cand) {
		t.Fatal("ExpireAfter=0 means never expire")
	}
}

func TestExpiration_Forceful(t *testing.T) {
	m := methods.NewExpiration()
	if !m.Forceful() {
		t.Fatal("Expiration must be Forceful")
	}
}

func TestExpiration_ComputeCommands_BypassBudget(t *testing.T) {
	cand := expirationCand("e", 2*time.Hour, time.Hour)
	// Pass an empty budget map; Forceful should ignore it.
	cmds, err := methods.NewExpiration().ComputeCommands(context.Background(), disruption.BudgetMap{}, cand)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("want 1 cmd despite zero budget, got %d", len(cmds))
	}
	if len(cmds[0].Replacements) != 1 {
		t.Fatalf("want 1 replacement, got %d", len(cmds[0].Replacements))
	}
}
