package scheduling

import (
	"fmt"
	"strconv"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Requirements is a typed slice with helpers.
type Requirements []v1alpha1.NodeSelectorRequirementWithMinValues

// Compatible returns nil iff every requirement in `pool` is satisfied by
// the constraints declared in `tunnel`. When both sides constrain the
// same key the operator pair must be compatible (see operatorsCompatible).
// Pool keys not mentioned by tunnel are treated as wildcard satisfied
// (the tunnel doesn't care about that dimension).
func Compatible(pool, tunnel Requirements) error {
	for _, p := range pool {
		for _, t := range tunnel {
			if t.Key != p.Key {
				continue
			}
			if !operatorsCompatible(p, t) {
				return fmt.Errorf("requirement %s: pool %v vs tunnel %v incompatible", p.Key, p, t)
			}
		}
	}
	return nil
}

// operatorsCompatible dispatches by pool operator to a per-operator
// helper. Mirrors Karpenter's pkg/scheduling/requirement.go logic.
// Split this way to keep individual cyclomatic complexity in check.
func operatorsCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	switch p.Operator {
	case v1alpha1.NodeSelectorOpIn:
		return inCompatible(p, t)
	case v1alpha1.NodeSelectorOpNotIn:
		return notInCompatible(p, t)
	case v1alpha1.NodeSelectorOpExists:
		return existsCompatible(t)
	case v1alpha1.NodeSelectorOpDoesNotExist:
		return t.Operator == v1alpha1.NodeSelectorOpDoesNotExist
	case v1alpha1.NodeSelectorOpGt:
		return gtCompatible(p, t)
	case v1alpha1.NodeSelectorOpLt:
		return ltCompatible(p, t)
	}
	return false
}

func inCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	switch t.Operator {
	case v1alpha1.NodeSelectorOpIn:
		return intersects(p.Values, t.Values)
	case v1alpha1.NodeSelectorOpNotIn:
		return hasValueNotIn(p.Values, t.Values)
	case v1alpha1.NodeSelectorOpExists:
		return true
	case v1alpha1.NodeSelectorOpDoesNotExist:
		return false
	case v1alpha1.NodeSelectorOpGt:
		return anyGreater(p.Values, t.Values)
	case v1alpha1.NodeSelectorOpLt:
		return anyLess(p.Values, t.Values)
	}
	return false
}

func notInCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	switch t.Operator {
	case v1alpha1.NodeSelectorOpIn:
		return hasValueNotIn(t.Values, p.Values)
	case v1alpha1.NodeSelectorOpNotIn, v1alpha1.NodeSelectorOpExists,
		v1alpha1.NodeSelectorOpDoesNotExist,
		v1alpha1.NodeSelectorOpGt, v1alpha1.NodeSelectorOpLt:
		return true
	}
	return false
}

func existsCompatible(t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	return t.Operator != v1alpha1.NodeSelectorOpDoesNotExist
}

func gtCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	pv, ok := firstInt(p.Values)
	if !ok {
		return false
	}
	switch t.Operator {
	case v1alpha1.NodeSelectorOpIn:
		return anyIntGreater(t.Values, pv)
	case v1alpha1.NodeSelectorOpGt, v1alpha1.NodeSelectorOpExists, v1alpha1.NodeSelectorOpNotIn:
		return true
	case v1alpha1.NodeSelectorOpLt:
		tv, ok := firstInt(t.Values)
		if !ok {
			return false
		}
		return pv < tv
	case v1alpha1.NodeSelectorOpDoesNotExist:
		return false
	}
	return false
}

func ltCompatible(p, t v1alpha1.NodeSelectorRequirementWithMinValues) bool {
	pv, ok := firstInt(p.Values)
	if !ok {
		return false
	}
	switch t.Operator {
	case v1alpha1.NodeSelectorOpIn:
		return anyIntLess(t.Values, pv)
	case v1alpha1.NodeSelectorOpLt, v1alpha1.NodeSelectorOpExists, v1alpha1.NodeSelectorOpNotIn:
		return true
	case v1alpha1.NodeSelectorOpGt:
		tv, ok := firstInt(t.Values)
		if !ok {
			return false
		}
		return tv < pv
	case v1alpha1.NodeSelectorOpDoesNotExist:
		return false
	}
	return false
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// hasValueNotIn reports whether `vals` contains any value not present in
// `forbidden`. Used for In × NotIn compatibility.
func hasValueNotIn(vals, forbidden []string) bool {
	for _, v := range vals {
		found := false
		for _, f := range forbidden {
			if v == f {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func firstInt(vals []string) (int, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(vals[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

func anyGreater(poolVals, tunnelVals []string) bool {
	tv, ok := firstInt(tunnelVals)
	if !ok {
		return false
	}
	return anyIntGreater(poolVals, tv)
}

func anyLess(poolVals, tunnelVals []string) bool {
	tv, ok := firstInt(tunnelVals)
	if !ok {
		return false
	}
	return anyIntLess(poolVals, tv)
}

func anyIntGreater(vals []string, threshold int) bool {
	for _, v := range vals {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		if n > threshold {
			return true
		}
	}
	return false
}

func anyIntLess(vals []string, threshold int) bool {
	for _, v := range vals {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		if n < threshold {
			return true
		}
	}
	return false
}
