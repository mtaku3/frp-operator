package methods

import (
	"crypto/rand"
	"encoding/hex"

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
	for k, v := range tmpl.Metadata.Labels {
		labels[k] = v
	}
	annotations := map[string]string{}
	for k, v := range tmpl.Metadata.Annotations {
		annotations[k] = v
	}
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

// randomSuffix returns 6 hex characters, suitable for ad-hoc claim names when
// GenerateName isn't usable (e.g. unit tests that pre-create the object).
func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
