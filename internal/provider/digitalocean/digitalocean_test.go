package digitalocean

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitalocean/godo"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// fakeDO returns an httptest.Server that mimics the godo endpoints we use.
// It tracks created droplets in-memory. Pass it as godo.Client.BaseURL.
type fakeDO struct {
	mu       sync.Mutex
	droplets map[int]*godo.Droplet
	nextID   int
}

func newFakeDO() *fakeDO {
	return &fakeDO{droplets: make(map[int]*godo.Droplet)}
}

func (f *fakeDO) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/droplets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req godo.DropletCreateRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.mu.Lock()
			f.nextID++
			id := f.nextID
			d := &godo.Droplet{
				ID:     id,
				Name:   req.Name,
				Status: "active",
				Networks: &godo.Networks{
					V4: []godo.NetworkV4{{IPAddress: fmt.Sprintf("203.0.113.%d", id), Type: "public"}},
				},
			}
			f.droplets[id] = d
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Droplet *godo.Droplet `json:"droplet"`
			}{Droplet: d})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v2/droplets/", func(w http.ResponseWriter, r *http.Request) {
		// /v2/droplets/<id>
		idStr := strings.TrimPrefix(r.URL.Path, "/v2/droplets/")
		id := 0
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		d, ok := f.droplets[id]
		f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Droplet *godo.Droplet `json:"droplet"`
			}{Droplet: d})
		case http.MethodDelete:
			f.mu.Lock()
			delete(f.droplets, id)
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func TestDigitalOcean_CreateInspectDestroy(t *testing.T) {
	fake := newFakeDO()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p, err := New(Config{
		Token:   "test-token",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	spec := provider.Spec{
		Name:              "ns__exit-1",
		Region:            "nyc1",
		Size:              "s-1vcpu-1gb",
		CloudInitUserData: []byte("#cloud-config\n"),
		Credentials:       []byte("test-token"),
	}
	st, err := p.Create(ctx, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if st.ProviderID == "" {
		t.Fatal("ProviderID empty")
	}
	if st.PublicIP == "" {
		t.Fatal("PublicIP empty")
	}
	if st.Phase != provider.PhaseRunning {
		t.Errorf("Phase: got %v want Running", st.Phase)
	}

	got, err := p.Inspect(ctx, st.ProviderID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.ProviderID != st.ProviderID {
		t.Errorf("Inspect ProviderID mismatch")
	}

	if err := p.Destroy(ctx, st.ProviderID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	if _, err := p.Inspect(ctx, st.ProviderID); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Inspect after Destroy: got %v, want ErrNotFound", err)
	}
}

func TestDigitalOcean_NameAndInterface(t *testing.T) {
	p, err := New(Config{Token: "x", BaseURL: "http://example.test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name() != "digitalocean" {
		t.Errorf("Name: got %q", p.Name())
	}
	var _ provider.Provisioner = p
}
