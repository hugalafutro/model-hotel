package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchModel PATCHes one model and returns the recorder, so the price-bound
// cases below stay one line each.
func patchModel(t *testing.T, r http.Handler, modelID, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

// readModel re-reads one model through GET /models (there is no per-id read
// route) so a persisted value can be told apart from one echoed off the
// request.
func readModel(t *testing.T, r http.Handler, modelID string) ModelResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list models status = %d: %s", rec.Code, rec.Body.String())
	}
	var models []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("decode model list: %v", err)
	}
	for _, m := range models {
		if m.ID == modelID {
			return m
		}
	}
	t.Fatalf("model %s missing from the list", modelID)
	return ModelResponse{}
}

// Each price field carries its own 0..1000 bound and its own rejection
// message. The messages matter: the operator only learns which of three
// look-alike fields they got wrong from the one that comes back, so a
// validator wired to the wrong field (or a message copied from the neighbour)
// has to fail here. The bodies are compared exactly, because "invalid input
// price" is a substring of "invalid cached input price".
func TestUpdateModel_PriceBoundsPerField(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"input below zero", `{"input_price_per_million": -0.5}`, "invalid input price"},
		{"input above ceiling", `{"input_price_per_million": 1000.5}`, "invalid input price"},
		{"cached input below zero", `{"input_price_per_million_cache_hit": -0.5}`, "invalid cached input price"},
		{"cached input above ceiling", `{"input_price_per_million_cache_hit": 1000.5}`, "invalid cached input price"},
		{"output below zero", `{"output_price_per_million": -0.5}`, "invalid output price"},
		{"output above ceiling", `{"output_price_per_million": 1000.5}`, "invalid output price"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, r := newTestHandlerWithRouter(t)
			modelID := createProviderAndModel(t, h, r)

			rec := patchModel(t, r, modelID, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.wantMsg {
				t.Errorf("message = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

// The ceiling is inclusive, so exactly 1000 has to be accepted: an off-by-one
// there would lock operators out of the top of the range with no way to tell
// it from a typo.
func TestUpdateModel_PriceCeilingIsInclusive(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	modelID := createProviderAndModel(t, h, r)

	rec := patchModel(t, r, modelID, `{"input_price_per_million_cache_hit": 1000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// The cached-input price is the newest of the three and the one most easily
// left out of the "did anything change?" precheck. If it were missing there,
// a request carrying only that field would be turned away as "no fields to
// update" and the price would silently never save.
func TestUpdateModel_CachedInputPriceAloneIsAChange(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	modelID := createProviderAndModel(t, h, r)

	rec := patchModel(t, r, modelID, `{"input_price_per_million_cache_hit": 0.25}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		CacheHit *float64 `json:"input_price_per_million_cache_hit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.CacheHit == nil || *got.CacheHit != 0.25 {
		t.Fatalf("cached input price = %v, want 0.25", got.CacheHit)
	}

	// And it is persisted, not just echoed back off the request.
	stored := readModel(t, r, modelID)
	if stored.InputPricePerMillionCacheHit == nil || *stored.InputPricePerMillionCacheHit != 0.25 {
		t.Fatalf("persisted cached input price = %v, want 0.25", stored.InputPricePerMillionCacheHit)
	}
}

// A rejected update must not have written anything: the 400 comes before the
// repository call, so every field the request touched stays as it was.
func TestUpdateModel_RejectedPriceLeavesModelUntouched(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	modelID := createProviderAndModel(t, h, r)

	if rec := patchModel(t, r, modelID, `{"display_name": "Kept", "input_price_per_million_cache_hit": 2}`); rec.Code != http.StatusOK {
		t.Fatalf("seed update status = %d: %s", rec.Code, rec.Body.String())
	}
	rec := patchModel(t, r, modelID, `{"display_name": "Clobbered", "input_price_per_million_cache_hit": -1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	stored := readModel(t, r, modelID)
	if stored.DisplayName != "Kept" {
		t.Errorf("display_name = %q, want %q: the rejected update wrote anyway", stored.DisplayName, "Kept")
	}
	if stored.InputPricePerMillionCacheHit == nil || *stored.InputPricePerMillionCacheHit != 2 {
		t.Errorf("cached input price = %v, want 2", stored.InputPricePerMillionCacheHit)
	}
}
