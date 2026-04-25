package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/serverinfo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret" {
			t.Errorf("missing or wrong basic auth: %v %v %v", u, p, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ServerInfo{
			Version:     "0.65.0",
			BindPort:    7000,
			ProxyCounts: map[string]int{"tcp": 3, "udp": 0},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	info, err := c.ServerInfo(context.Background())
	if err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	if info.Version != "0.65.0" {
		t.Errorf("Version: got %q", info.Version)
	}
	if info.ProxyCounts["tcp"] != 3 {
		t.Errorf("ProxyCounts[tcp]: got %d, want 3", info.ProxyCounts["tcp"])
	}
}

func TestPutConfigSendsBodyAndReloads(t *testing.T) {
	var seenPaths []string
	var seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			b, _ := io.ReadAll(r.Body)
			seenBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/reload":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	body := []byte(`bindPort = 7000` + "\n" + `[auth]` + "\n" + `token = "x"` + "\n")
	if err := c.PutConfigAndReload(context.Background(), body); err != nil {
		t.Fatalf("PutConfigAndReload: %v", err)
	}
	want := []string{"PUT /api/config", "GET /api/reload"}
	if len(seenPaths) != 2 || seenPaths[0] != want[0] || seenPaths[1] != want[1] {
		t.Errorf("call sequence: got %v want %v", seenPaths, want)
	}
	if !strings.Contains(seenBody, "bindPort = 7000") {
		t.Errorf("body not forwarded: %q", seenBody)
	}
}

func TestServerInfoBadAuthReturnsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "wrong")
	_, err := c.ServerInfo(context.Background())
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error should mention 401: %v", err)
	}
}
