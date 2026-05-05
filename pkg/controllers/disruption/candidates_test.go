package disruption_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func candPool() *v1alpha1.ExitPool { return &v1alpha1.ExitPool{ObjectMeta: metav1.ObjectMeta{Name: "p1"}} }

func candReadyClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{v1alpha1.LabelExitPool: "p1"},
		},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: "fake://" + name,
			Conditions: []metav1.Condition{{
				Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(), Reason: "R",
			}},
		},
	}
}

func TestCandidates_FiltersNotReady(t *testing.T) {
	c := state.NewCluster(nil)
	notReady := candReadyClaim("e1")
	notReady.Status.Conditions = nil
	c.UpdateExit(notReady)
	c.UpdateExit(candReadyClaim("e2"))
	cands := disruption.GetCandidates(c, func(string) *v1alpha1.ExitPool { return candPool() })
	if len(cands) != 1 || cands[0].Claim.Name != "e2" {
		t.Fatalf("expected only e2, got %d candidates", len(cands))
	}
}

func TestCandidates_FiltersDoNotDisrupt(t *testing.T) {
	c := state.NewCluster(nil)
	dnd := candReadyClaim("e1")
	dnd.Annotations = map[string]string{v1alpha1.AnnotationDoNotDisrupt: "true"}
	c.UpdateExit(dnd)
	c.UpdateExit(candReadyClaim("e2"))
	cands := disruption.GetCandidates(c, func(string) *v1alpha1.ExitPool { return candPool() })
	if len(cands) != 1 || cands[0].Claim.Name != "e2" {
		t.Fatalf("expected only e2, got %d candidates", len(cands))
	}
}

func TestCandidates_FiltersMarkedForDeletion(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdateExit(candReadyClaim("e1"))
	c.UpdateExit(candReadyClaim("e2"))
	c.MarkExitForDeletion("e1")
	cands := disruption.GetCandidates(c, func(string) *v1alpha1.ExitPool { return candPool() })
	if len(cands) != 1 || cands[0].Claim.Name != "e2" {
		t.Fatalf("expected only e2, got %d", len(cands))
	}
}

func TestCandidates_FiltersMissingPool(t *testing.T) {
	c := state.NewCluster(nil)
	c.UpdateExit(candReadyClaim("e1"))
	cands := disruption.GetCandidates(c, func(string) *v1alpha1.ExitPool { return nil })
	if len(cands) != 0 {
		t.Fatal("missing pool should drop candidate")
	}
}
