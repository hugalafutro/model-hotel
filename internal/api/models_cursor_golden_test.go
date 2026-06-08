package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestListModelsCursor_Golden pins the full ModelsCursorResponse over a
// hand-controlled dataset: default (name asc) ordering + total, a provider_id
// filter (with total parity), and forward and backward cursor pages (the
// backward case exercises the fetch-inverted + slice-reverse path). It is the
// safety net for the ListModelsCursor decomposition — in particular the
// scanModelRow + modelToResponse swap, which must reproduce every field exactly.
//
// Six models are seeded under a fresh provider (so the assertions are isolated
// via provider_id): names m0..m5, sorted ascending by name.
func TestListModelsCursor_Golden(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	providerData := fmt.Sprintf(`{"name": "golden-models-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	preq.Header.Set("Authorization", "Bearer test-admin-token")
	preq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, preq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create provider: %d: %s", rec.Code, rec.Body.String())
	}
	var pr struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil {
		t.Fatalf("parse provider: %v", err)
	}

	pool := h.Pool().Pool()
	names := []string{"gm0", "gm1", "gm2", "gm3", "gm4", "gm5"}
	for i, name := range names {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, capabilities, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), pr.ID, name, name, `{"vision": true}`, true); err != nil {
			t.Fatalf("insert model %d: %v", i, err)
		}
	}

	doReq := func(q string) ModelsCursorResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/models/cursor?"+q, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET ?%s: status %d: %s", q, w.Code, w.Body.String())
		}
		var resp ModelsCursorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ?%s: %v", q, err)
		}
		return resp
	}
	namesOf := func(resp ModelsCursorResponse) []string {
		out := make([]string, len(resp.Entries))
		for i, e := range resp.Entries {
			out[i] = e.Name
		}
		return out
	}
	cursorFrom := func(e ModelResponse) string {
		c := modelCursor{SortBy: "name", Name: e.Name, ModelID: e.ModelID, ID: e.ID}
		return url.QueryEscape(c.encode())
	}

	// 1) Default page (name asc), filtered to our provider: gm0..gm5 in order.
	def := doReq("provider_id=" + pr.ID)
	if got := namesOf(def); !slices.Equal(got, names) {
		t.Errorf("default page names = %v, want %v", got, names)
	}
	if def.Total != 6 {
		t.Errorf("default total = %d, want 6", def.Total)
	}
	if def.HasBefore {
		t.Error("default page: HasBefore = true, want false")
	}

	// 2) Forward (after) from entry[1] (gm1) -> strictly later window gm2..gm5.
	after := doReq("provider_id=" + pr.ID + "&direction=after&sort_dir=asc&cursor=" + cursorFrom(def.Entries[1]))
	if got := namesOf(after); !slices.Equal(got, names[2:]) {
		t.Errorf("after-cursor names = %v, want %v", got, names[2:])
	}

	// 3) Backward (before) from entry[3] (gm3) -> earlier window gm0..gm2 in asc
	//    order (fetched descending then reversed).
	before := doReq("provider_id=" + pr.ID + "&direction=before&sort_dir=asc&cursor=" + cursorFrom(def.Entries[3]))
	if got := namesOf(before); !slices.Equal(got, names[0:3]) {
		t.Errorf("before-cursor names = %v, want %v", got, names[0:3])
	}
}
