package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestGetAppLogsCursor_Golden pins the full AppLogsCursorResponse over a
// hand-controlled dataset: default-page ordering + total, a level filter (with
// total parity), and forward and backward cursor pages (the backward case
// exercises the fetch-inverted + slice-reverse path). It is the safety net for
// the app-log helper extraction — appendAppLogFilters, appendAppLogKeysetPredicate,
// and paginateAppLogs must not change any of these outcomes.
//
// Six rows are seeded under a unique source (isolated from any other data):
// index 0 is newest .. index 5 oldest; rows 0–2 are "info", rows 3–5 "error".
func TestGetAppLogsCursor_Golden(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	source := "goldsrc-" + uuid.New().String()[:8]
	ids := make([]string, 6) // newest (0) -> oldest (5)
	for i := 0; i < 6; i++ {
		id := uuid.New().String()
		ids[i] = id
		level := "info"
		if i >= 3 {
			level = "error"
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, NOW() - ($6 * INTERVAL '1 minute'), $2, $3, $4, NOW() - ($5 * INTERVAL '1 minute'))`,
			id, level, source, "golden message", i, i); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	doReq := func(q string) AppLogsCursorResponse {
		t.Helper()
		req := httptest.NewRequest("GET", "/logs/app/cursor?"+q, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET ?%s: status %d: %s", q, w.Code, w.Body.String())
		}
		var resp AppLogsCursorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ?%s: %v", q, err)
		}
		return resp
	}
	idsOf := func(resp AppLogsCursorResponse) []string {
		out := make([]string, len(resp.Entries))
		for i, e := range resp.Entries {
			out[i] = e.ID
		}
		return out
	}
	cursorFrom := func(e AppLogEntry) string {
		ts, err := time.Parse(time.RFC3339Nano, e.CreatedAt)
		if err != nil {
			t.Fatalf("parse created_at %q: %v", e.CreatedAt, err)
		}
		c := appLogCursor{CreatedAt: ts, ID: e.ID}
		return url.QueryEscape(c.encode())
	}

	// 1) Default page (desc), filtered to our source: all 6 newest->oldest.
	def := doReq("source=" + source)
	if got := idsOf(def); !slices.Equal(got, ids) {
		t.Errorf("default page ids = %v, want %v", got, ids)
	}
	if def.Total != 6 {
		t.Errorf("default total = %d, want 6", def.Total)
	}
	if def.HasBefore {
		t.Error("default page: HasBefore = true, want false")
	}

	// 2) level=error -> rows 3,4,5 only; count honors the same filter.
	errOnly := doReq("source=" + source + "&level=error")
	if got := idsOf(errOnly); !slices.Equal(got, ids[3:]) {
		t.Errorf("level=error ids = %v, want %v", got, ids[3:])
	}
	if errOnly.Total != 3 {
		t.Errorf("level=error total = %d, want 3", errOnly.Total)
	}

	// 3) Forward (after) from entry[1] -> strictly older window ids[2:].
	after := doReq("source=" + source + "&direction=after&sort_dir=desc&cursor=" + cursorFrom(def.Entries[1]))
	if got := idsOf(after); !slices.Equal(got, ids[2:]) {
		t.Errorf("after-cursor ids = %v, want %v", got, ids[2:])
	}

	// 4) Backward (before) from entry[3] -> newer window ids[0:3] in desc order
	//    (fetched ascending then reversed).
	before := doReq("source=" + source + "&direction=before&sort_dir=desc&cursor=" + cursorFrom(def.Entries[3]))
	if got := idsOf(before); !slices.Equal(got, ids[0:3]) {
		t.Errorf("before-cursor ids = %v, want %v", got, ids[0:3])
	}
}
