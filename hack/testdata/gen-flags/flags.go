package operator

import "flag"

type Config struct {
	LeaderElection bool
	LogLevel       string
}

func LoadConfigFromArgs(args []string) (*Config, error) {
	cfg := &Config{}
	fs := flag.NewFlagSet("frp-operator", flag.ContinueOnError)
	fs.BoolVar(&cfg.LeaderElection, "leader-elect", false, "Enable leader election.")
	fs.StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug|info|warn|error).")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) error {
	return nil
}
