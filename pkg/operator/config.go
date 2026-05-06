package operator

import "time"

// Config mirrors the settings surface of spec §12. Defaults applied by
// LoadConfig if neither env nor flag overrides them.
type Config struct {
	KubeConfig       string
	LeaderElection   bool
	LeaderElectionID string
	LeaderElectionNS string
	// DisableProfiling is parsed but not yet wired — Phase 9 has no
	// pprof endpoint to gate. Reserved for a later phase.
	DisableProfiling bool

	BatchIdleDuration time.Duration
	BatchMaxDuration  time.Duration
	KubeClientQPS     float32
	KubeClientBurst   int

	// LogLevel is parsed but not yet wired — Phase 9 inherits the logger
	// configured by the manager binary. Reserved for a later phase.
	LogLevel        string
	MetricsAddr     string
	HealthProbeAddr string

	PreferencePolicy string // Respect | Ignore
	// MinValuesPolicy is parsed but not yet wired — the scheduler does
	// not enforce minValues constraints in Phase 9. Reserved for a
	// later phase.
	MinValuesPolicy string // Strict | BestEffort

	RegistrationTTL      time.Duration
	DriftTTL             time.Duration
	DisruptionPollPeriod time.Duration

	FeatureGates map[string]bool
}

// Defaults captures the spec §12 defaults.
func Defaults() *Config {
	return &Config{
		LeaderElection:       true,
		LeaderElectionID:     "frp-operator.frp.operator.io",
		LeaderElectionNS:     "frp-operator-system",
		DisableProfiling:     false,
		BatchIdleDuration:    1 * time.Second,
		BatchMaxDuration:     10 * time.Second,
		KubeClientQPS:        200,
		KubeClientBurst:      300,
		LogLevel:             "info",
		MetricsAddr:          ":8080",
		HealthProbeAddr:      ":8081",
		PreferencePolicy:     "Respect",
		MinValuesPolicy:      "Strict",
		RegistrationTTL:      15 * time.Minute,
		DriftTTL:             15 * time.Minute,
		DisruptionPollPeriod: 10 * time.Second,
		FeatureGates:         defaultFeatureGates(),
	}
}
