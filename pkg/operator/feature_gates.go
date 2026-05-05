package operator

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Known feature gates per spec §11. All default to false.
var knownFeatureGates = []string{
	"StaticReplicas",
	"ExitRepair",
	"InterruptionHandling",
	"ConsolidationDryRun",
	"MultiPoolBinpacking",
}

func defaultFeatureGates() map[string]bool {
	out := map[string]bool{}
	for _, g := range knownFeatureGates {
		out[g] = false
	}
	return out
}

// KnownFeatureGates returns the sorted list of recognized gate names.
func KnownFeatureGates() []string {
	out := append([]string(nil), knownFeatureGates...)
	sort.Strings(out)
	return out
}

// ParseFeatureGates parses a comma-separated list "Name=true,Name2=false".
// Empty input yields the defaults map. Unknown gates → error.
func ParseFeatureGates(spec string) (map[string]bool, error) {
	out := defaultFeatureGates()
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return out, nil
	}
	known := map[string]struct{}{}
	for _, k := range knownFeatureGates {
		known[k] = struct{}{}
	}
	for _, kv := range strings.Split(spec, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("feature-gates: malformed %q (expected Name=bool)", kv)
		}
		name := strings.TrimSpace(parts[0])
		val, err := strconv.ParseBool(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("feature-gates: %q has non-bool value: %w", name, err)
		}
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("feature-gates: unknown gate %q (known: %v)", name, KnownFeatureGates())
		}
		out[name] = val
	}
	return out, nil
}
