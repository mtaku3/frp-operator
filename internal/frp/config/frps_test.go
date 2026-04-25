package config

import "testing"

func TestRenderFrpsDefault(t *testing.T) {
	cfg := FrpsConfig{
		BindPort: 7000,
		Auth:     FrpsAuth{Method: "token", Token: "exit-token-xyz"},
		WebServer: FrpsWebServer{
			Addr:     "0.0.0.0",
			Port:     7500,
			User:     "admin",
			Password: "admin-password-xyz",
		},
		AllowPorts: []FrpsPortRange{
			{Single: 80},
			{Single: 443},
			{Start: 1024, End: 65535},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frps_default.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frps_default output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
