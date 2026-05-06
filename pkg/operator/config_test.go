package operator

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.LeaderElectionNS != "frp-operator-system" {
		t.Errorf("ns: %q", c.LeaderElectionNS)
	}
	if c.MetricsAddr != ":8080" {
		t.Errorf("metrics: %q", c.MetricsAddr)
	}
	if c.HealthProbeAddr != ":8081" {
		t.Errorf("probe: %q", c.HealthProbeAddr)
	}
	if c.BatchIdleDuration != 1*time.Second {
		t.Errorf("idle: %v", c.BatchIdleDuration)
	}
	for _, g := range KnownFeatureGates() {
		if c.FeatureGates[g] {
			t.Errorf("default gate %q should be false", g)
		}
	}
}

func TestLoadConfigFlagOverridesDefaults(t *testing.T) {
	t.Setenv("METRICS_BIND_ADDRESS", "")
	cfg, err := LoadConfigFromArgs([]string{
		"--metrics-bind-address=:9090",
		"--leader-elect=false",
		"--batch-idle-duration=5s",
		"--feature-gates=ExitRepair=true,StaticReplicas=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != ":9090" {
		t.Errorf("metrics: %q", cfg.MetricsAddr)
	}
	if cfg.LeaderElection {
		t.Error("expected leader-elect=false")
	}
	if cfg.BatchIdleDuration != 5*time.Second {
		t.Errorf("idle: %v", cfg.BatchIdleDuration)
	}
	if !cfg.FeatureGates["ExitRepair"] {
		t.Error("ExitRepair should be true")
	}
	if cfg.FeatureGates["StaticReplicas"] {
		t.Error("StaticReplicas should be false")
	}
}

func TestLoadConfigEnvOverridesDefault(t *testing.T) {
	t.Setenv("METRICS_BIND_ADDRESS", ":7777")
	t.Setenv("BATCH_MAX_DURATION", "30s")
	t.Setenv("KUBE_CLIENT_QPS", "42")
	cfg, err := LoadConfigFromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != ":7777" {
		t.Errorf("metrics: %q", cfg.MetricsAddr)
	}
	if cfg.BatchMaxDuration != 30*time.Second {
		t.Errorf("max: %v", cfg.BatchMaxDuration)
	}
	if cfg.KubeClientQPS != 42 {
		t.Errorf("qps: %v", cfg.KubeClientQPS)
	}
}

func TestFlagOverridesEnv(t *testing.T) {
	t.Setenv("METRICS_BIND_ADDRESS", ":7777")
	cfg, err := LoadConfigFromArgs([]string{"--metrics-bind-address=:9090"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != ":9090" {
		t.Errorf("flag should beat env, got %q", cfg.MetricsAddr)
	}
}

func TestParseFeatureGates(t *testing.T) {
	gates, err := ParseFeatureGates("")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range KnownFeatureGates() {
		if _, ok := gates[k]; !ok {
			t.Errorf("missing default gate %q", k)
		}
	}

	gates, err = ParseFeatureGates("ExitRepair=true,InterruptionHandling=false,ConsolidationDryRun=true,MultiPoolBinpacking=false,StaticReplicas=true")
	if err != nil {
		t.Fatal(err)
	}
	if !gates["ExitRepair"] || gates["InterruptionHandling"] || !gates["ConsolidationDryRun"] || gates["MultiPoolBinpacking"] || !gates["StaticReplicas"] {
		t.Errorf("gate parse mismatch: %v", gates)
	}

	if _, err := ParseFeatureGates("Bogus=true"); err == nil {
		t.Error("expected error for unknown gate")
	}
	if _, err := ParseFeatureGates("ExitRepair=notbool"); err == nil {
		t.Error("expected error for non-bool")
	}
	if _, err := ParseFeatureGates("ExitRepair"); err == nil {
		t.Error("expected error for missing =bool")
	}
}
