package scheduling

import (
	corev1 "k8s.io/api/core/v1"
)

// ResourcesFit reports whether `requested` ResourceList can be subtracted
// from `available` without going negative on any dimension. Missing
// dimensions in `available` mean "unbounded for that dimension."
func ResourcesFit(available, requested corev1.ResourceList) bool {
	for k, req := range requested {
		avail, ok := available[k]
		if !ok {
			continue
		}
		if avail.Cmp(req) < 0 {
			return false
		}
	}
	return true
}

// Subtract returns available - requested for the dimensions in available.
func Subtract(available, requested corev1.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{}
	for k, v := range available {
		cur := v.DeepCopy()
		if r, ok := requested[k]; ok {
			cur.Sub(r)
		}
		out[k] = cur
	}
	return out
}

// Sum returns the dimension-wise sum.
func Sum(lists ...corev1.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{}
	for _, l := range lists {
		for k, v := range l {
			cur, ok := out[k]
			if !ok {
				out[k] = v.DeepCopy()
				continue
			}
			cur.Add(v)
			out[k] = cur
		}
	}
	return out
}
