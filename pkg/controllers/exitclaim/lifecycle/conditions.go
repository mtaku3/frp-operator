package lifecycle

import (
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// isCondTrue reports whether the named condition is currently True.
func isCondTrue(claim *v1alpha1.ExitClaim, t string) bool {
	return apimeta.IsStatusConditionTrue(claim.Status.Conditions, t)
}

// setCond sets-or-appends a condition on the claim's status.
func setCond(claim *v1alpha1.ExitClaim, t string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               t,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

// condTransitionTime returns the LastTransitionTime of the named condition,
// or the zero time when absent.
func condTransitionTime(claim *v1alpha1.ExitClaim, t string) time.Time {
	c := apimeta.FindStatusCondition(claim.Status.Conditions, t)
	if c == nil {
		return time.Time{}
	}
	return c.LastTransitionTime.Time
}
