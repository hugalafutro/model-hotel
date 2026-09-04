package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

func TestShouldRediscover(t *testing.T) {
	base := func() *provider.Provider {
		return &provider.Provider{ID: uuid.New(), BaseURL: "https://api.openai.com/v1", ProviderType: "openai", Enabled: true, AutodiscoveryEnabled: true}
	}
	cases := []struct {
		name       string
		noPrior    bool
		prior      func(p *provider.Provider)
		updated    func(p *provider.Provider)
		keyChanged bool
		want       bool
	}{
		{name: "rename only", want: false},
		{name: "enable transition", prior: func(p *provider.Provider) { p.Enabled = false }, want: true},
		{name: "autodiscovery switched on", prior: func(p *provider.Provider) { p.AutodiscoveryEnabled = false }, want: true},
		{name: "disable transition", updated: func(p *provider.Provider) { p.Enabled = false }, want: false},
		{name: "base url change", updated: func(p *provider.Provider) { p.BaseURL = "https://other.example.com/v1" }, want: true},
		{name: "type change", updated: func(p *provider.Provider) { p.ProviderType = "custom" }, want: true},
		{name: "key change", keyChanged: true, want: true},
		{name: "key change while disabled", updated: func(p *provider.Provider) { p.Enabled = false }, keyChanged: true, want: false},
		{name: "url change with autodiscovery off", updated: func(p *provider.Provider) {
			p.BaseURL = "https://other.example.com/v1"
			p.AutodiscoveryEnabled = false
		}, want: false},
		{name: "no prior row", noPrior: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var prior *provider.Provider
			if !tc.noPrior {
				prior = base()
				if tc.prior != nil {
					tc.prior(prior)
				}
			}
			updated := base()
			if tc.updated != nil {
				tc.updated(updated)
			}
			if got := shouldRediscover(prior, updated, tc.keyChanged); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// TestUpdateProvider_BaseURLChangeRediscovers verifies a save that moves a
// provider to another address rediscovers its catalogue on this node without a
// Discover click, the way a config-sync import does on fleet members.
func TestUpdateProvider_BaseURLChangeRediscovers(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	listing := func(modelID string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/v1/models" {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": modelID, "owned_by": "test", "object": "model"}},
			})
		}))
	}
	old := listing("original-model-a")
	defer old.Close()
	moved := listing("moved-model-a")
	defer moved.Close()

	create := fmt.Sprintf(`{"name":"rediscover-on-move","base_url":"%s/v1","api_key":"sk-test"}`, old.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(create))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(created.ID)

	// Create runs no discovery server-side, so any model row below comes from
	// the update.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+created.ID, strings.NewReader(fmt.Sprintf(`{"base_url":"%s/v1"}`, moved.URL)))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// last_discovered_at is the scan's final write, so once it is stamped the
	// background goroutine has nothing left to touch in the shared test DB.
	deadline := time.Now().Add(5 * time.Second)
	for {
		prov, err := h.providerRepo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get provider: %v", err)
		}
		if prov.LastDiscoveredAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background discovery did not stamp last_discovered_at")
		}
		time.Sleep(20 * time.Millisecond)
	}
	models, err := newModelRepo(h.dbPool.Pool()).List(context.Background(), &id)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "moved-model-a" {
		t.Fatalf("want the moved listing, got %d models", len(models))
	}
}
