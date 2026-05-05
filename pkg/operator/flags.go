package operator

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// LoadConfig builds a Config from defaults, then env vars, then CLI flags
// (last writer wins). Caller passes os.Args[1:] (LoadConfigFromArgs) or
// nil (parse os.Args). Returns parsed config or an error.
func LoadConfig() (*Config, error) {
	return LoadConfigFromArgs(os.Args[1:])
}

// LoadConfigFromArgs is the testable entry point.
func LoadConfigFromArgs(args []string) (*Config, error) {
	cfg := Defaults()

	// Apply env overrides first.
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}

	fs := flag.NewFlagSet("frp-operator", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.StringVar(&cfg.KubeConfig, "kubeconfig", cfg.KubeConfig, "Path to kubeconfig (empty = in-cluster).")
	fs.BoolVar(&cfg.LeaderElection, "leader-elect", cfg.LeaderElection, "Enable leader election.")
	fs.StringVar(&cfg.LeaderElectionID, "leader-election-id", cfg.LeaderElectionID, "Leader election lock name.")
	fs.StringVar(&cfg.LeaderElectionNS, "leader-election-namespace", cfg.LeaderElectionNS,
		"Leader election lock namespace.")
	fs.BoolVar(&cfg.DisableProfiling, "disable-profiling", cfg.DisableProfiling, "Disable pprof endpoints.")

	fs.DurationVar(&cfg.BatchIdleDuration, "batch-idle-duration", cfg.BatchIdleDuration, "Idle window before batch fires.")
	fs.DurationVar(&cfg.BatchMaxDuration, "batch-max-duration", cfg.BatchMaxDuration, "Max time a batch may stay open.")

	var qps float64 = float64(cfg.KubeClientQPS)
	fs.Float64Var(&qps, "kube-client-qps", qps, "Sustained kube client QPS.")
	fs.IntVar(&cfg.KubeClientBurst, "kube-client-burst", cfg.KubeClientBurst, "Burst kube client QPS.")

	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Log level (debug|info|warn|error).")
	fs.StringVar(&cfg.MetricsAddr, "metrics-bind-address", cfg.MetricsAddr, "Address the metrics server listens on.")
	fs.StringVar(&cfg.HealthProbeAddr, "health-probe-bind-address", cfg.HealthProbeAddr,
		"Address the health probe listens on.")

	fs.StringVar(&cfg.PreferencePolicy, "preference-policy", cfg.PreferencePolicy,
		"Scheduler preference policy: Respect | Ignore.")
	fs.StringVar(&cfg.MinValuesPolicy, "min-values-policy", cfg.MinValuesPolicy,
		"Scheduler MinValues policy: Strict | BestEffort.")

	fs.DurationVar(&cfg.RegistrationTTL, "registration-ttl", cfg.RegistrationTTL,
		"How long to wait for a fresh exit to register.")
	fs.DurationVar(&cfg.DriftTTL, "drift-ttl", cfg.DriftTTL,
		"How long a drift must persist before disruption fires.")
	fs.DurationVar(&cfg.DisruptionPollPeriod, "disruption-poll-period", cfg.DisruptionPollPeriod,
		"Disruption controller tick.")

	var gateSpec string
	fs.StringVar(&gateSpec, "feature-gates", "", "Comma-separated list of Name=bool gate overrides.")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.KubeClientQPS = float32(qps)

	if gateSpec != "" {
		gates, err := ParseFeatureGates(gateSpec)
		if err != nil {
			return nil, err
		}
		cfg.FeatureGates = gates
	}

	return cfg, nil
}

func applyEnv(cfg *Config) error {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		cfg.KubeConfig = v
	}
	if v, ok := boolEnv("LEADER_ELECT"); ok {
		cfg.LeaderElection = v
	}
	if v := os.Getenv("LEADER_ELECTION_ID"); v != "" {
		cfg.LeaderElectionID = v
	}
	if v := os.Getenv("LEADER_ELECTION_NAMESPACE"); v != "" {
		cfg.LeaderElectionNS = v
	}
	if v, ok := boolEnv("DISABLE_PROFILING"); ok {
		cfg.DisableProfiling = v
	}
	if v, ok := durEnv("BATCH_IDLE_DURATION"); ok {
		cfg.BatchIdleDuration = v
	}
	if v, ok := durEnv("BATCH_MAX_DURATION"); ok {
		cfg.BatchMaxDuration = v
	}
	if v, ok := floatEnv("KUBE_CLIENT_QPS"); ok {
		cfg.KubeClientQPS = float32(v)
	}
	if v, ok := intEnv("KUBE_CLIENT_BURST"); ok {
		cfg.KubeClientBurst = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("METRICS_BIND_ADDRESS"); v != "" {
		cfg.MetricsAddr = v
	}
	if v := os.Getenv("HEALTH_PROBE_BIND_ADDRESS"); v != "" {
		cfg.HealthProbeAddr = v
	}
	if v := os.Getenv("PREFERENCE_POLICY"); v != "" {
		cfg.PreferencePolicy = v
	}
	if v := os.Getenv("MIN_VALUES_POLICY"); v != "" {
		cfg.MinValuesPolicy = v
	}
	if v, ok := durEnv("REGISTRATION_TTL"); ok {
		cfg.RegistrationTTL = v
	}
	if v, ok := durEnv("DRIFT_TTL"); ok {
		cfg.DriftTTL = v
	}
	if v, ok := durEnv("DISRUPTION_POLL_PERIOD"); ok {
		cfg.DisruptionPollPeriod = v
	}
	if v := os.Getenv("FEATURE_GATES"); v != "" {
		gates, err := ParseFeatureGates(v)
		if err != nil {
			return fmt.Errorf("FEATURE_GATES env var: %w", err)
		}
		cfg.FeatureGates = gates
	}
	return nil
}

func boolEnv(key string) (bool, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return false, false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return v, true
}

func intEnv(key string) (int, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

func floatEnv(key string) (float64, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func durEnv(key string) (time.Duration, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}
