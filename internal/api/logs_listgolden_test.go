package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// TestListLogs_Golden pins the full LogsResponse for the offset list endpoint
// over a hand-controlled dataset: default-page ordering + total, a status filter
// (with total parity), and a second page. It is the safety net for the ListLogs
// helper-adoption refactor — the shared scan dests (logEntryScanDests), the
// shared SELECT projection, and appendLogFilters must not change any of these
// outcomes.
//
// Six rows are seeded under a unique model_id (isolated from any other data):
// index 0 is newest .. index 5 oldest; rows 0–2 are 200, rows 3–5 are 500.
func TestListLogs_Golden(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	provID := uuid.New()
	insertTestProvider(t, pool, provID, "listgold-prov-"+uuid.New().String()[:8], "https://lg.example/v1")

	model := "listgold-" + uuid.New().String()[:8]
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

	// ListLogs uses globalLogsCache (keyed by raw query). Each query below has a
	// unique model_id, so keys never collide, but clear it defensively.
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	doReq := func(q string) LogsResponse {
		t.Helper()
		req := httptest.NewRequest("GET", "/logs?"+q, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET ?%s: status %d: %s", q, w.Code, w.Body.String())
		}
		var resp LogsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ?%s: %v", q, err)
		}
		return resp
	}
	idsOf := func(resp LogsResponse) []string {
		out := make([]string, len(resp.Entries))
		for i, e := range resp.Entries {
			out[i] = e.ID
		}
		return out
	}

	// 1) Default page (sort=time desc), filtered to our model: all 6 newest->oldest.
	def := doReq("model_id=" + model)
	if got := idsOf(def); !slices.Equal(got, ids) {
		t.Errorf("default page ids = %v, want %v", got, ids)
	}
	if def.Total != 6 {
		t.Errorf("default total = %d, want 6", def.Total)
	}
	if def.Page != 1 || def.PerPage != 20 {
		t.Errorf("default page/per_page = %d/%d, want 1/20", def.Page, def.PerPage)
	}

	// 2) status_code=5xx -> rows 3,4,5 only; total honors the same filter.
	fiveXX := doReq("model_id=" + model + "&status_code=5xx")
	if got := idsOf(fiveXX); !slices.Equal(got, ids[3:]) {
		t.Errorf("5xx page ids = %v, want %v", got, ids[3:])
	}
	if fiveXX.Total != 3 {
		t.Errorf("5xx total = %d, want 3", fiveXX.Total)
	}

	// 3) Second page (per_page=2, page=2) -> ids[2:4]; total still 6.
	page2 := doReq("model_id=" + model + "&per_page=2&page=2")
	if got := idsOf(page2); !slices.Equal(got, ids[2:4]) {
		t.Errorf("page 2 ids = %v, want %v", got, ids[2:4])
	}
	if page2.Total != 6 || page2.Page != 2 || page2.PerPage != 2 {
		t.Errorf("page 2 total/page/per_page = %d/%d/%d, want 6/2/2", page2.Total, page2.Page, page2.PerPage)
	}
}
