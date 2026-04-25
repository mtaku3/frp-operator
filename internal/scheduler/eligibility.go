package scheduler

import (
	"strconv"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PortsFit returns true iff every requested public port is free on exit:
// not in spec.ReservedPorts and not already allocated in status.Allocations.
// An empty ports slice trivially fits.
func PortsFit(exit frpv1alpha1.ExitServer, ports []int32) bool {
	reserved := make(map[int32]struct{}, len(exit.Spec.ReservedPorts))
	for _, p := range exit.Spec.ReservedPorts {
		reserved[p] = struct{}{}
	}
	for _, p := range ports {
		if _, isReserved := reserved[p]; isReserved {
			return false
		}
		if _, allocated := exit.Status.Allocations[strconv.Itoa(int(p))]; allocated {
			return false
		}
	}
	return true
}

// CapacityFits returns true iff adding `req` to `exit.Status.Usage` stays
// at or below `exit.Spec.Capacity` for every dimension. Unset capacity
// dimensions are unbounded; unset requirement dimensions count as zero.
func CapacityFits(exit frpv1alpha1.ExitServer, req frpv1alpha1.TunnelRequirements) bool {
	cap := exit.Spec.Capacity
	use := exit.Status.Usage
	if cap == nil {
		return true
	}
	if cap.MaxTunnels != nil && use.Tunnels+1 > *cap.MaxTunnels {
		return false
	}
	if cap.MonthlyTrafficGB != nil {
		var add int64
		if req.MonthlyTrafficGB != nil {
			add = *req.MonthlyTrafficGB
		}
		if use.MonthlyTrafficGB+add > *cap.MonthlyTrafficGB {
			return false
		}
	}
	if cap.BandwidthMbps != nil {
		var add int32
		if req.BandwidthMbps != nil {
			add = *req.BandwidthMbps
		}
		if use.BandwidthMbps+add > *cap.BandwidthMbps {
			return false
		}
	}
	return true
}

// PlacementMatches returns true iff exit satisfies the soft preferences in
// placement. A nil placement is treated as no constraints. Lists are
// match-any: the exit qualifies if it appears in the list (or the list is
// empty for that dimension).
func PlacementMatches(exit frpv1alpha1.ExitServer, p *frpv1alpha1.Placement) bool {
	if p == nil {
		return true
	}
	if len(p.Providers) > 0 && !containsProvider(p.Providers, exit.Spec.Provider) {
		return false
	}
	if len(p.Regions) > 0 && !containsString(p.Regions, exit.Spec.Region) {
		return false
	}
	return true
}

func containsProvider(haystack []frpv1alpha1.Provider, needle frpv1alpha1.Provider) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// EligibleExits returns the subset of exits that are ready, port-compatible,
// capacity-compatible, and match the tunnel's placement preferences. The
// caller (Allocator) ranks among these.
func EligibleExits(exits []frpv1alpha1.ExitServer, t *frpv1alpha1.Tunnel) []frpv1alpha1.ExitServer {
	ports := tunnelPorts(t)
	var req frpv1alpha1.TunnelRequirements
	if t.Spec.Requirements != nil {
		req = *t.Spec.Requirements
	}
	out := make([]frpv1alpha1.ExitServer, 0, len(exits))
	for _, e := range exits {
		if e.Status.Phase != frpv1alpha1.PhaseReady {
			continue
		}
		if !PlacementMatches(e, t.Spec.Placement) {
			continue
		}
		if !PortsFit(e, ports) {
			continue
		}
		if !CapacityFits(e, req) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// tunnelPorts returns the public ports requested by the tunnel. PublicPort
// defaults to ServicePort when unset.
func tunnelPorts(t *frpv1alpha1.Tunnel) []int32 {
	out := make([]int32, 0, len(t.Spec.Ports))
	for _, p := range t.Spec.Ports {
		if p.PublicPort != nil {
			out = append(out, *p.PublicPort)
		} else {
			out = append(out, p.ServicePort)
		}
	}
	return out
}
