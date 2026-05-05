package methods

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
)

// replacementForCandidate returns a fresh ExitClaim spec'd from the
// candidate's pool template. Drift methods stamp the new pool-hash annotation
// so the next reconcile won't immediately mark it drifted again. The returned
// claim has no metadata.Name yet — the queue assigns one at create time.
func replacementForCandidate(c *disruption.Candidate) *v1alpha1.ExitClaim {
	if c == nil || c.Pool == nil {
		return nil
	}
	pool := c.Pool
	tmpl := pool.Spec.Template
	labels := map[string]string{v1alpha1.LabelExitPool: pool.Name}
	maps.Copy(labels, tmpl.Metadata.Labels)
	annotations := map[string]string{}
	maps.Copy(annotations, tmpl.Metadata.Annotations)
	if h := pool.Annotations[v1alpha1.AnnotationPoolHash]; h != "" {
		annotations[v1alpha1.AnnotationPoolHash] = h
	}
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Labels:       labels,
			Annotations:  annotations,
		},
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef:       tmpl.Spec.ProviderClassRef,
			Requirements:           tmpl.Spec.Requirements,
			Frps:                   tmpl.Spec.Frps,
			ExpireAfter:            tmpl.Spec.ExpireAfter,
			TerminationGracePeriod: tmpl.Spec.TerminationGracePeriod,
		},
	}
}
