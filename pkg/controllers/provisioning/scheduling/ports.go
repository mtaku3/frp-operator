package scheduling

import (
	"strconv"
	"strings"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PortsFit reports whether every requested port can be allocated on the
// exit. Each entry with PublicPort=nil/0 consumes one auto-assign slot
// from the free port set.
func PortsFit(allowPorts []string, reserved []int32, used map[int32]struct{}, requested []v1alpha1.TunnelPort) bool {
	_, ok := ResolveAutoAssign(allowPorts, reserved, used, requested)
	return ok
}

// ResolveAutoAssign returns a port assignment for each requested port
// (in input order). Specific PublicPort values must already be present
// in the free set; PublicPort=nil/0 entries draw from the lowest free
// remaining ports. Returns nil,false when not allocatable.
func ResolveAutoAssign(allowPorts []string, reserved []int32, used map[int32]struct{}, requested []v1alpha1.TunnelPort) ([]int32, bool) {
	free := computeFree(allowPorts, reserved, used)
	out := make([]int32, len(requested))
	autoIdx := []int{}
	// Pass 1: claim explicit ports.
	for i, p := range requested {
		if p.PublicPort == nil || *p.PublicPort == 0 {
			autoIdx = append(autoIdx, i)
			continue
		}
		want := *p.PublicPort
		if _, ok := free[want]; !ok {
			return nil, false
		}
		delete(free, want)
		out[i] = want
	}
	if len(autoIdx) == 0 {
		return out, true
	}
	// Pass 2: assign autos from sorted-ascending free pool.
	freeSorted := sortedKeys(free)
	if len(freeSorted) < len(autoIdx) {
		return nil, false
	}
	for n, idx := range autoIdx {
		out[idx] = freeSorted[n]
	}
	return out, true
}

// computeFree expands AllowPorts ranges minus Reserved minus used.
func computeFree(allowPorts []string, reserved []int32, used map[int32]struct{}) map[int32]struct{} {
	free := map[int32]struct{}{}
	for _, spec := range allowPorts {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		if i := strings.IndexByte(spec, '-'); i >= 0 {
			lo, errLo := strconv.Atoi(strings.TrimSpace(spec[:i]))
			hi, errHi := strconv.Atoi(strings.TrimSpace(spec[i+1:]))
			if errLo != nil || errHi != nil || lo > hi {
				continue
			}
			for p := lo; p <= hi; p++ {
				free[int32(p)] = struct{}{}
			}
			continue
		}
		n, err := strconv.Atoi(spec)
		if err != nil {
			continue
		}
		free[int32(n)] = struct{}{}
	}
	for _, r := range reserved {
		delete(free, r)
	}
	for u := range used {
		delete(free, u)
	}
	return free
}

func sortedKeys(m map[int32]struct{}) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Insertion sort — port sets are small.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
