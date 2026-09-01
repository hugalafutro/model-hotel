package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The provider list and detail carry the proxy's last cap note for a provider
// that has one, and omit the field for one that has not (and when no ledger
// is wired at all).
func TestProviders_LastCapOverlay(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)
	create := func(name string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/providers", strings.NewReader(`{"name": "`+name+`", "base_url": "https://ollama.com", "api_key": "test-api-key"}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
		var out struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.ID
	}
	capped := create("capped")
	quiet := create("quiet")
	get := func(path string) []byte {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}

	// No ledger wired: the field is absent everywhere.
	if body := get("/providers"); strings.Contains(string(body), "last_cap") {
		t.Errorf("last_cap present with no ledger: %s", body)
	}

	ledger := provider.NewCapLedger()
	at := time.Date(2026, 8, 31, 14, 51, 0, 0, time.UTC)
	ledger.Note(uuid.MustParse(capped), provider.CapNote{Phrase: "session usage limit", Model: "gpt-oss:120b", Status: 429, At: at})
	h.SetCapLedger(ledger)

	var list []struct {
		ID      string            `json:"id"`
		LastCap *provider.CapNote `json:"last_cap"`
	}
	if err := json.Unmarshal(get("/providers"), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, p := range list {
		switch p.ID {
		case capped:
			if p.LastCap == nil || p.LastCap.Phrase != "session usage limit" || p.LastCap.Model != "gpt-oss:120b" || !p.LastCap.At.Equal(at) {
				t.Errorf("capped provider last_cap = %+v", p.LastCap)
			}
		case quiet:
			if p.LastCap != nil {
				t.Errorf("quiet provider has a last_cap: %+v", p.LastCap)
			}
		}
	}
	var one struct {
		LastCap *provider.CapNote `json:"last_cap"`
	}
	if err := json.Unmarshal(get("/providers/"+capped), &one); err != nil || one.LastCap == nil || one.LastCap.Status != 429 {
		t.Errorf("detail last_cap = %+v (%v)", one.LastCap, err)
	}
}
