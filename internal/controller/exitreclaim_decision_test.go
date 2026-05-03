package controller

import (
	"testing"
	"time"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mkExit(phase frpv1alpha1.ExitPhase, allocs int, opts ...func(*frpv1alpha1.ExitServer)) *frpv1alpha1.ExitServer {
	e := &frpv1alpha1.ExitServer{
		Spec: frpv1alpha1.ExitServerSpec{},
		Status: frpv1alpha1.ExitServerStatus{
			Phase:       phase,
			Allocations: map[string]string{},
		},
	}
	for i := range allocs {
		e.Status.Allocations[strconvI(20000+i)] = "ns/x"
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func strconvI(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestReclaimDecision(t *testing.T) {
	drainAfter := 10 * time.Minute
	now := metav1.Now()
	earlier := metav1.Time{Time: now.Add(-15 * time.Minute)}

	cases := []struct {
		name           string
		exit           *frpv1alpha1.ExitServer
		reclaimEnabled bool
		want           reclaimAction
	}{
		{
			name:           "ready with no allocations and policy enabled -> StartDrain",
			exit:           mkExit(frpv1alpha1.PhaseReady, 0),
			reclaimEnabled: true,
			want:           reclaimActionStartDrain,
		},
		{
			name:           "ready with allocations -> NoOp",
			exit:           mkExit(frpv1alpha1.PhaseReady, 1),
			reclaimEnabled: true,
			want:           reclaimActionNoOp,
		},
		{
			name:           "ready with no allocations but policy disabled -> NoOp",
			exit:           mkExit(frpv1alpha1.PhaseReady, 0),
			reclaimEnabled: false,
			want:           reclaimActionNoOp,
		},
		{
			name: "ready with no allocations but per-exit annotation disabled -> NoOp",
			exit: mkExit(frpv1alpha1.PhaseReady, 0, func(e *frpv1alpha1.ExitServer) {
				e.Annotations = map[string]string{"frp-operator.io/reclaim": "false"}
			}),
			reclaimEnabled: true,
			want:           reclaimActionNoOp,
		},
		{
			name: "draining with allocations again -> AbortDrain",
			exit: mkExit(frpv1alpha1.PhaseDraining, 1, func(e *frpv1alpha1.ExitServer) {
				e.Status.DrainStartedAt = &now
			}),
			reclaimEnabled: true,
			want:           reclaimActionAbortDrain,
		},
		{
			name: "draining within drainAfter and still empty -> RequeueDrain",
			exit: mkExit(frpv1alpha1.PhaseDraining, 0, func(e *frpv1alpha1.ExitServer) {
				e.Status.DrainStartedAt = &now
			}),
			reclaimEnabled: true,
			want:           reclaimActionRequeueDrain,
		},
		{
			name: "draining past drainAfter and still empty -> Destroy",
			exit: mkExit(frpv1alpha1.PhaseDraining, 0, func(e *frpv1alpha1.ExitServer) {
				e.Status.DrainStartedAt = &earlier
			}),
			reclaimEnabled: true,
			want:           reclaimActionDestroy,
		},
		{
			name:           "lost or failed -> NoOp",
			exit:           mkExit(frpv1alpha1.PhaseLost, 0),
			reclaimEnabled: true,
			want:           reclaimActionNoOp,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideReclaim(tc.exit, tc.reclaimEnabled, drainAfter, now.Time)
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
