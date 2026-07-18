package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// TestListLogsCursor_ParamEdges covers the parameter-handling edges of the
// cursor list endpoint that the golden test doesn't: the limit clamps
// ([1,200]), the invalid-cursor → 400 path, and the backward+ascending fetch
// (which inverts the sort to "desc" in buildLogListQuery).
func TestListLogsCursor_ParamEdges(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	provID := uuid.New()
	insertTestProvider(t, pool, provID, "edge-prov-"+uuid.New().String()[:8], "https://e.example/v1")

	model := "edge-" + uuid.New().String()[:8]
	for i := range 3 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
			 VALUES ($1, $2, $3, 200, 100, NOW() - ($4 * INTERVAL '1 minute'))`,
			uuid.New(), provID, model, i); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	get := func(q string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/logs/cursor?"+q, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Invalid cursor -> 400 (parseLogListParams decode error).
	if w := get("cursor=not%21base64"); w.Code != http.StatusBadRequest {
		t.Errorf("invalid cursor: status %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// limit clamps: 0 -> 1, 500 -> 200; both must still succeed.
	for _, lim := range []string{"0", "500"} {
		if w := get("model_id=" + model + "&limit=" + lim); w.Code != http.StatusOK {
			t.Errorf("limit=%s: status %d, want 200 (%s)", lim, w.Code, w.Body.String())
		}
	}

	// Backward (before) + ascending sort: exercises buildLogListQuery's
	// fetch-sort inversion (asc -> desc) for the backward window.
	def := get("model_id=" + model + "&sort_dir=asc")
	if def.Code != http.StatusOK {
		t.Fatalf("asc default: status %d (%s)", def.Code, def.Body.String())
	}
	var resp LogsCursorResponse
	if err := json.Unmarshal(def.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode asc default: %v", err)
	}
	if len(resp.Entries) < 2 {
		t.Fatalf("asc default: got %d entries, want >= 2", len(resp.Entries))
	}
	c := logCursor{CreatedAt: resp.Entries[1].CreatedAt, ID: resp.Entries[1].ID}
	if w := get("model_id=" + model + "&direction=before&sort_dir=asc&cursor=" + url.QueryEscape(c.encode())); w.Code != http.StatusOK {
		t.Errorf("before+asc: status %d, want 200 (%s)", w.Code, w.Body.String())
	}
}
