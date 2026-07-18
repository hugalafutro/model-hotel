package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// TestListLogsCursor_Golden pins the full cursor-pagination behavior over a
// hand-controlled dataset: ordering, a filter (+ count parity), and forward
// and backward cursor pages (the backward case exercises the fetch-inverted +
// slice-reverse path). It is the safety net for the ListLogsCursor
// decomposition — the helpers it extracts (filters, keyset predicate, scan)
// must not change any of these outcomes.
//
// Six rows are seeded under a unique model_id (so the assertions are isolated
// from any other data): index 0 is newest .. index 5 oldest; rows 0–2 are 200,
// rows 3–5 are 500.
func TestListLogsCursor_Golden(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	provID := uuid.New()
	insertTestProvider(t, pool, provID, "golden-prov-"+uuid.New().String()[:8], "https://g.example/v1")

	model := "golden-" + uuid.New().String()[:8]
	ids := make([]string, 6) // newest (0) -> oldest (5)
	for i := range 6 {
		id := uuid.New()
		ids[i] = id.String()
		status := 200
		if i >= 3 {
			status = 500
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
			 VALUES ($1, $2, $3, $4, 100, NOW() - ($5 * INTERVAL '1 minute'))`,
			id, provID, model, status, i); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	// ListLogsCursor does not use globalLogsCache, but clear it defensively.
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	doReq := func(q string) LogsCursorResponse {
		t.Helper()
		req := httptest.NewRequest("GET", "/logs/cursor?"+q, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET ?%s: status %d: %s", q, w.Code, w.Body.String())
		}
		var resp LogsCursorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ?%s: %v", q, err)
		}
		return resp
	}
	idsOf := func(resp LogsCursorResponse) []string {
		out := make([]string, len(resp.Entries))
		for i, e := range resp.Entries {
			out[i] = e.ID
		}
		return out
	}
	cursorFrom := func(e LogEntry) string {
		c := logCursor{CreatedAt: e.CreatedAt, ID: e.ID}
		return url.QueryEscape(c.encode())
	}

	// 1) Default page (desc), filtered to our model: all 6 newest->oldest.
	def := doReq("model_id=" + model)
	if got := idsOf(def); !slices.Equal(got, ids) {
		t.Errorf("default page ids = %v, want %v", got, ids)
	}
	if def.Total != 6 {
		t.Errorf("default total = %d, want 6", def.Total)
	}
	if def.HasBefore {
		t.Error("default page: HasBefore = true, want false")
	}

	// 2) status_code=5xx → rows 3,4,5 only; count honors the same filter.
	fiveXX := doReq("model_id=" + model + "&status_code=5xx")
	if got := idsOf(fiveXX); !slices.Equal(got, ids[3:]) {
		t.Errorf("5xx page ids = %v, want %v", got, ids[3:])
	}
	if fiveXX.Total != 3 {
		t.Errorf("5xx total = %d, want 3", fiveXX.Total)
	}

	// 3) Forward (after) from entry[1] → strictly older window ids[2:].
	after := doReq("model_id=" + model + "&direction=after&sort_dir=desc&cursor=" + cursorFrom(def.Entries[1]))
	if got := idsOf(after); !slices.Equal(got, ids[2:]) {
		t.Errorf("after-cursor ids = %v, want %v", got, ids[2:])
	}

	// 4) Backward (before) from entry[3] → newer window ids[0:3] in desc order
	//    (fetched ascending then reversed).
	before := doReq("model_id=" + model + "&direction=before&sort_dir=desc&cursor=" + cursorFrom(def.Entries[3]))
	if got := idsOf(before); !slices.Equal(got, ids[0:3]) {
		t.Errorf("before-cursor ids = %v, want %v", got, ids[0:3])
	}
}
