// Package scheduling: offering selection.
//
// Karpenter scheduler picks an InstanceType + Offering for each new
// NodeClaim by intersecting NodePool requirements, pod requirements,
// and the cloudprovider catalog, then sorting the survivors by
// Offering.Price ascending. SelectInstanceType is the frp-operator
// equivalent: cheapest valid (instance-type, offering) for the pool +
// requested resources + pinned tunnel requirements.
package scheduling

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// candidate pairs an InstanceType with one of its Offerings (the
// cheapest one whose requirements satisfy the pool + tunnel filters).
type candidate struct {
	IT       *cloudprovider.InstanceType
	Offering *cloudprovider.Offering
}

// SelectInstanceType returns the cheapest (InstanceType, Offering) pair
// that satisfies pool + tunnel requirements and fits `requested`.
// Returns an error when no candidate fits — caller treats that as
// "no pool can produce a claim".
func SelectInstanceType(
	ctx context.Context,
	reg *cloudprovider.Registry,
	pool *v1alpha1.ExitPool,
	tunnelReqs []v1alpha1.NodeSelectorRequirementWithMinValues,
	requested corev1.ResourceList,
) (*cloudprovider.InstanceType, *cloudprovider.Offering, error) {
	if reg == nil {
		return nil, nil, fmt.Errorf("scheduling: cloudprovider registry not configured")
	}
	cp, err := reg.For(pool.Spec.Template.Spec.ProviderClassRef.Kind)
	if err != nil {
		return nil, nil, err
	}
	its, err := cp.GetInstanceTypes(ctx, pool)
	if err != nil {
		return nil, nil, fmt.Errorf("get instance types: %w", err)
	}
	if len(its) == 0 {
		return nil, nil, fmt.Errorf("scheduling: pool %q has empty instance-type catalog", pool.Name)
	}

	poolReqs := pool.Spec.Template.Spec.Requirements
	candidates := make([]candidate, 0, len(its))
	for _, it := range its {
		if Compatible(poolReqs, it.Requirements) != nil {
			continue
		}
		if Compatible(tunnelReqs, it.Requirements) != nil {
			continue
		}
		if !ResourcesFit(it.Allocatable(), requested) {
			continue
		}
		off := cheapestCompatibleOffering(it, poolReqs, tunnelReqs)
		if off == nil {
			continue
		}
		candidates = append(candidates, candidate{IT: it, Offering: off})
	}
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("scheduling: no instance type fits pool %q", pool.Name)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Offering.Price < candidates[j].Offering.Price
	})
	c := candidates[0]
	return c.IT, c.Offering, nil
}

func cheapestCompatibleOffering(
	it *cloudprovider.InstanceType, poolReqs, tunnelReqs []v1alpha1.NodeSelectorRequirementWithMinValues,
) *cloudprovider.Offering {
	var best *cloudprovider.Offering
	for _, off := range it.Offerings {
		if !off.Available {
			continue
		}
		if Compatible(poolReqs, off.Requirements) != nil {
			continue
		}
		if Compatible(tunnelReqs, off.Requirements) != nil {
			continue
		}
		if best == nil || off.Price < best.Price {
			best = off
		}
	}
	return best
}

// PinChosen returns the union of InstanceType.Requirements and
// Offering.Requirements as additions to a claim spec. Karpenter's
// equivalent narrows NodeClaim.Spec.Requirements to the chosen
// dimensions so the cloudprovider Create has no ambiguity.
func PinChosen(
	it *cloudprovider.InstanceType, off *cloudprovider.Offering,
) []v1alpha1.NodeSelectorRequirementWithMinValues {
	out := make([]v1alpha1.NodeSelectorRequirementWithMinValues, 0, len(it.Requirements)+len(off.Requirements))
	seen := map[string]struct{}{}
	add := func(r v1alpha1.NodeSelectorRequirementWithMinValues) {
		if _, dup := seen[r.Key]; dup {
			return
		}
		seen[r.Key] = struct{}{}
		out = append(out, r)
	}
	for _, r := range it.Requirements {
		add(r)
	}
	for _, r := range off.Requirements {
		add(r)
	}
	return out
}
