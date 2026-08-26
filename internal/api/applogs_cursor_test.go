package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// The cursor and history endpoints: paging in both directions, the filters
// they take and the date-range boundary.

func TestGetAppLogsHistory_NilDBPool(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(resp.Entries))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestGetAppLogsHistory_NilDBPool_JSONEncodeError(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	w := &brokenResponseWriter{header: make(http.Header)}

	// Should not panic, just log the error
	h.getAppLogsHistory(w, req)
}

func TestGetAppLogsHistory_InvalidPage(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&page=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_InvalidPerPage(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&per_page=200", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_ToParam(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&to=2024-12-31T23:59:59Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_SortByAndDir(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&sort_by=time&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The handler returns an error message in the body (status 200)
	// Note: handler doesn't set 500 status, just returns error JSON
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	// Verify error response is returned
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestGetAppLogsCursor_Default(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with different timestamp and created_at values
	for i := range 5 {
		logID := uuid.New().String()
		eventTs := time.Now().Add(-time.Duration(i) * time.Minute).UTC()
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			logID,
			eventTs.Format(time.RFC3339Nano),
			"info",
			"test",
			fmt.Sprintf("test message %d", i),
			createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Test default cursor request (no cursor)
	req := httptest.NewRequest("GET", "/logs/app/cursor", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Entries) == 0 {
		t.Error("expected entries to be returned")
	}
	if resp.Total < 5 {
		t.Errorf("expected total >= 5, got %d", resp.Total)
	}
	// First page should have has_before=false (nothing newer)
	if resp.HasBefore {
		t.Error("expected HasBefore=false for first page")
	}
	// Verify level_counts and source_counts are present
	if resp.LevelCounts == nil {
		t.Error("expected LevelCounts to be non-nil")
	}
	if resp.SourceCounts == nil {
		t.Error("expected SourceCounts to be non-nil")
	}
}

func TestGetAppLogsCursor_WithCursor(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with distinct timestamps (1 day apart)
	// Use different values for timestamp (event time) and created_at (insertion time)
	// to ensure cursor pagination uses created_at, not timestamp
	now := time.Now().UTC()
	for i := range 5 {
		logID := uuid.New().String()
		eventTs := now.Add(-time.Duration(i) * 24 * time.Hour)
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			logID,
			eventTs.Format(time.RFC3339Nano),
			"info",
			"test",
			fmt.Sprintf("test message %d", i),
			createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// First request to get initial page
	req := httptest.NewRequest("GET", "/logs/app/cursor?limit=2", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var firstResp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	if len(firstResp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(firstResp.Entries))
	}
	if firstResp.HasBefore {
		t.Error("expected HasBefore=false for first page (no cursor)")
	}

	// Build a cursor from the last entry's created_at (insertion time, not event timestamp)
	lastEntry := firstResp.Entries[len(firstResp.Entries)-1]
	cursorCat, err := time.Parse(time.RFC3339Nano, lastEntry.CreatedAt)
	if err != nil {
		t.Fatalf("failed to parse cursor created_at: %v", err)
	}
	cursor := appLogCursor{
		CreatedAt: cursorCat,
		ID:        lastEntry.ID,
	}
	cursorStr := cursor.encode()

	// Second request with cursor - verify has_before is set
	req = httptest.NewRequest("GET", "/logs/app/cursor?cursor="+url.QueryEscape(cursorStr)+"&limit=2", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var secondResp AppLogsCursorResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}

	// Key assertion: has_before should be true when cursor is provided
	if !secondResp.HasBefore {
		t.Error("expected HasBefore=true when using cursor")
	}
	// Response should still have valid structure
	if secondResp.LevelCounts == nil {
		t.Error("expected LevelCounts to be non-nil")
	}
	if secondResp.SourceCounts == nil {
		t.Error("expected SourceCounts to be non-nil")
	}
}

func TestGetAppLogsCursor_InvalidCursor(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	_, r := newTestHandlerWithRouter(t)

	// Test with invalid base64 cursor
	req := httptest.NewRequest("GET", "/logs/app/cursor?cursor=not-valid-base64", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid cursor, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		// respondBadRequest returns plain text, not JSON
		if w.Body.String() == "" {
			t.Error("expected error message for invalid cursor")
		}
	} else if resp["error"] == "" && resp["message"] == "" {
		t.Error("expected error message for invalid cursor")
	}
}

func TestGetAppLogsCursor_WithFilters(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with different levels and sources
	testCases := []struct {
		level  string
		source string
		msg    string
	}{
		{"info", "proxy", "proxy info message"},
		{"warning", "auth", "auth warning message"},
		{"error", "proxy", "proxy error message"},
		{"info", "discovery", "discovery info message"},
	}

	for _, tc := range testCases {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW())`,
			uuid.New().String(),
			time.Now().UTC().Format(time.RFC3339Nano),
			tc.level,
			tc.source,
			tc.msg)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Test level filter
	req := httptest.NewRequest("GET", "/logs/app/cursor?level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("level filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.Level != "error" {
			t.Errorf("expected level 'error', got %q", entry.Level)
		}
	}

	// Test source filter
	req = httptest.NewRequest("GET", "/logs/app/cursor?source=proxy", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("source filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.Source != "proxy" {
			t.Errorf("expected source 'proxy', got %q", entry.Source)
		}
	}
}

// TestGetAppLogsCursor_BackwardPagination tests that direction=before returns
// the items immediately preceding the cursor, not items from the start of
// the dataset, and that results are in the requested sort order.
// ---------------------------------------------------------------------------
// appendAppLogFilters unit tests
// ---------------------------------------------------------------------------

func TestBuildAppLogCursorQuery_NoCursorNoFilters(t *testing.T) {
	p := appLogCursorParams{
		limit:     20,
		sortDir:   "DESC",
		direction: "after",
	}
	q := url.Values{}
	query, args := buildAppLogCursorQuery(p, q)

	if !strings.Contains(query, "SELECT id, created_at, timestamp, level, source, message, escaped, attrs_at FROM app_logs") {
		t.Errorf("expected SELECT from app_logs, got %q", query)
	}
	if !strings.Contains(query, "ORDER BY created_at DESC, id DESC") {
		t.Errorf("expected ORDER BY created_at DESC, id DESC, got %q", query)
	}
	if !strings.Contains(query, "LIMIT") {
		t.Errorf("expected LIMIT clause, got %q", query)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg (limit+1), got %d", len(args))
	}
	if args[0] != 21 { // limit+1
		t.Errorf("expected limit arg 21, got %v", args[0])
	}
}

func TestBuildAppLogCursorQuery_WithFilters(t *testing.T) {
	p := appLogCursorParams{
		limit:     10,
		sortDir:   "DESC",
		direction: "after",
	}
	q := url.Values{
		"level":  {"error"},
		"source": {"proxy"},
	}
	query, _ := buildAppLogCursorQuery(p, q)

	if !strings.Contains(query, "WHERE") {
		t.Errorf("expected WHERE clause with filters, got %q", query)
	}
	if !strings.Contains(query, "level = $1") {
		t.Errorf("expected level filter, got %q", query)
	}
	if !strings.Contains(query, "source = $2") {
		t.Errorf("expected source filter, got %q", query)
	}
}

func TestBuildAppCursorQuery_WithCursor(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "cursor-id"}
	cursorStr := cursor.encode()
	p := appLogCursorParams{
		limit:     20,
		sortDir:   "DESC",
		direction: "after",
		cursorStr: cursorStr,
		cursor:    cursor,
	}
	q := url.Values{}
	query, args := buildAppLogCursorQuery(p, q)

	if !strings.Contains(query, "WHERE") {
		t.Errorf("expected WHERE clause with keyset predicate, got %q", query)
	}
	if !strings.Contains(query, "created_at < $1") {
		t.Errorf("after+DESC should produce '< for keyset, got %q", query)
	}
	if len(args) != 4 { // 3 from keyset + 1 from limit
		t.Fatalf("expected 4 args, got %d", len(args))
	}
}

func TestBuildAppLogCursorQuery_BackwardDescInvertsSort(t *testing.T) {
	p := appLogCursorParams{
		limit:     20,
		sortDir:   "DESC",
		direction: "before",
	}
	q := url.Values{}
	query, _ := buildAppLogCursorQuery(p, q)

	if !strings.Contains(query, "ORDER BY created_at ASC, id ASC") {
		t.Errorf("before+DESC should invert to ASC sort in fetch query, got %q", query)
	}
}

func TestBuildAppLogCursorQuery_BackwardAscInvertsSort(t *testing.T) {
	p := appLogCursorParams{
		limit:     20,
		sortDir:   "ASC",
		direction: "before",
	}
	q := url.Values{}
	query, _ := buildAppLogCursorQuery(p, q)

	if !strings.Contains(query, "ORDER BY created_at DESC, id DESC") {
		t.Errorf("before+ASC should invert to DESC sort in fetch query, got %q", query)
	}
}

func TestGetAppLogsCursor_BackwardPagination(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	now := time.Now().UTC()
	ids := make([]string, 10)
	for i := range 10 {
		ids[i] = uuid.New().String()
		eventTs := now.Add(-time.Duration(i) * time.Hour)
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			ids[i], eventTs.Format(time.RFC3339Nano), "info", "test",
			fmt.Sprintf("backward-msg-%d", i), createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log %d: %v", i, err)
		}
	}

	// Page 1 DESC (newest 3)
	req := httptest.NewRequest("GET", "/logs/app/cursor?limit=3&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("failed to decode page1: %v", err)
	}
	if len(page1.Entries) != 3 {
		t.Fatalf("expected 3 entries on page1, got %d", len(page1.Entries))
	}

	// Page 2
	page1Last := page1.Entries[len(page1.Entries)-1]
	cursor1Cat, _ := time.Parse(time.RFC3339Nano, page1Last.CreatedAt)
	cursor1 := appLogCursor{CreatedAt: cursor1Cat, ID: page1Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor1.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page2: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("failed to decode page2: %v", err)
	}
	if len(page2.Entries) != 3 {
		t.Fatalf("expected 3 entries on page2, got %d", len(page2.Entries))
	}

	// Page 3
	page2Last := page2.Entries[len(page2.Entries)-1]
	cursor2Cat, _ := time.Parse(time.RFC3339Nano, page2Last.CreatedAt)
	cursor2 := appLogCursor{CreatedAt: cursor2Cat, ID: page2Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor2.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page3: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page3 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page3); err != nil {
		t.Fatalf("failed to decode page3: %v", err)
	}
	if len(page3.Entries) != 3 {
		t.Fatalf("expected 3 entries on page3, got %d", len(page3.Entries))
	}

	// Backward from page3's first entry — should return page2's entries
	backwardCat, _ := time.Parse(time.RFC3339Nano, page3.Entries[0].CreatedAt)
	backwardCursor := appLogCursor{CreatedAt: backwardCat, ID: page3.Entries[0].ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=before", url.QueryEscape(backwardCursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("backward page: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var beforePage AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &beforePage); err != nil {
		t.Fatalf("failed to decode backward page: %v", err)
	}

	if len(beforePage.Entries) != 3 {
		t.Fatalf("expected 3 entries for backward page, got %d", len(beforePage.Entries))
	}

	// Results must match page2 entries (DESC order)
	if beforePage.Entries[0].ID != page2.Entries[0].ID {
		t.Errorf("expected first entry ID %s, got %s", page2.Entries[0].ID, beforePage.Entries[0].ID)
	}
	if beforePage.Entries[1].ID != page2.Entries[1].ID {
		t.Errorf("expected second entry ID %s, got %s", page2.Entries[1].ID, beforePage.Entries[1].ID)
	}
	if beforePage.Entries[2].ID != page2.Entries[2].ID {
		t.Errorf("expected third entry ID %s, got %s", page2.Entries[2].ID, beforePage.Entries[2].ID)
	}

	if !beforePage.HasAfter {
		t.Error("expected HasAfter=true for backward page with cursor")
	}
	if !beforePage.HasBefore {
		t.Error("expected HasBefore=true for backward page (more items precede)")
	}
}

// ---------------------------------------------------------------------------
// getAppLogsHistory edge cases
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_EmptyLogs verifies that getAppLogsHistory returns
// a valid response with zero entries and zero total when no logs exist.
func TestGetAppLogsHistory_EmptyLogs(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(resp.Entries))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
	if resp.Page != 1 {
		t.Errorf("expected page 1, got %d", resp.Page)
	}
}

// TestGetAppLogsHistory_SingleEntry verifies that getAppLogsHistory returns
// the correct pagination when there is exactly one log entry.
func TestGetAppLogsHistory_SingleEntry(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Clean up test data
	pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'single-entry-test'")
	defer pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'single-entry-test'")

	// Insert a single log entry
	_, err := pool.Exec(context.Background(),
		`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New().String(),
		time.Now().UTC().Format(time.RFC3339Nano),
		"error",
		"single-entry-test",
		"single test message")
	if err != nil {
		t.Fatalf("Failed to insert app log: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total < 1 {
		t.Errorf("expected total >= 1, got %d", resp.Total)
	}
	if len(resp.Entries) == 0 {
		t.Error("expected at least one entry")
	}
}

// TestGetAppLogsHistory_DateRangeBoundary verifies that getAppLogsHistory
// correctly filters by from/to date range parameters.
func TestGetAppLogsHistory_DateRangeBoundary(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert log entries with different timestamps
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	twoDaysAgo := now.Add(-48 * time.Hour)

	for i, ts := range []time.Time{twoDaysAgo, yesterday, now} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New().String(),
			ts.Format(time.RFC3339Nano),
			"info",
			"date-range-test",
			fmt.Sprintf("entry %d", i),
			ts)
		if err != nil {
			t.Fatalf("Failed to insert app log %d: %v", i, err)
		}
	}
	defer pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'date-range-test'")

	// Query with from=12h ago — should only include entries from the last 12h
	from := now.Add(-12 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&from="+url.QueryEscape(from), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Parse the from boundary so we can compare timestamps.
	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		t.Fatalf("failed to parse from time: %v", err)
	}

	// Verify that no entries from our test data with source="date-range-test"
	// have a timestamp older than the from boundary.
	for _, e := range resp.Entries {
		if e.Source != "date-range-test" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			t.Errorf("failed to parse entry timestamp %q: %v", e.Timestamp, err)
			continue
		}
		if ts.Before(fromTime) {
			t.Errorf("entry with timestamp %s is before from boundary %s, but should have been filtered out", ts.Format(time.RFC3339), fromTime.Format(time.RFC3339))
		}
	}

	// Verify that at least the "now" entry is present.
	foundNow := false
	for _, e := range resp.Entries {
		if e.Source == "date-range-test" && e.Message == "entry 2" {
			foundNow = true
		}
	}
	if !foundNow {
		t.Error("expected 'entry 2' (the 'now' entry) to be present in results")
	}
}

// TestGetAppLogsCursor_NilPool tests that GetAppLogsCursor returns an empty
// cursor response when the handler has no database pool (nil dbPool early return).
func TestGetAppLogsCursor_NilPool(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/logs/app/cursor", http.NoBody)
	w := httptest.NewRecorder()
	h.GetAppLogsCursor(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(resp.Entries))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
	// Nil pool returns zeroed response; LevelCounts/SourceCounts will be nil
	// which JSON encodes as null — this is the expected nil-pool behavior.
}

// TestGetAppLogsCursor_CancelledContext tests that GetAppLogsCursor returns
// a 500 error when the request context is already cancelled.
func TestGetAppLogsCursor_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	_, r := newTestHandlerWithRouter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/logs/app/cursor", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// scanAppLogRow unit tests
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_QueryFailsWithCancelledContext verifies getAppLogsHistory
// when the DB query fails after countAppLogs succeeds. Uses a cancelled context
// to trigger the query failure path.
func TestGetAppLogsHistory_QueryFailsWithCancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// Create a db.DB and close it to force the query to fail
	ctx := context.Background()
	testDB, err := db.New(ctx, apiTestDBURL, 5, 1)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	testDB.Close()

	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, nil, nil, auth, testDB)

	req := httptest.NewRequest("GET", "/app-logs/history", http.NoBody)
	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	// Should return an error JSON response (countAppLogs fails with closed pool)
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, hasError := resp["error"]; !hasError {
		t.Error("expected error response for closed pool in getAppLogsHistory")
	}
}

// ---------------------------------------------------------------------------
// appLogCursor encode/decode tests
// ---------------------------------------------------------------------------

// TestAppLogCursor_EncodeDecode verifies that encode/decode round-trips correctly.
func TestAppLogCursor_EncodeDecode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	c := &appLogCursor{
		CreatedAt: now,
		ID:        "test-id-123",
	}

	encoded := c.encode()
	if encoded == "" {
		t.Error("expected non-empty encoded cursor")
	}

	decoded := &appLogCursor{}
	if err := decoded.decode(encoded); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.ID != "test-id-123" {
		t.Errorf("expected ID 'test-id-123', got %q", decoded.ID)
	}
}

// TestAppLogCursor_DecodeInvalidBase64 tests that decode fails for invalid base64.
func TestAppLogCursor_DecodeInvalidBase64(t *testing.T) {
	c := &appLogCursor{}
	if err := c.decode("not-valid-base64!!!"); err == nil {
		t.Error("expected error for invalid base64")
	}
}

// TestAppLogCursor_DecodeInvalidJSON tests that decode fails for valid base64
// that doesn't contain valid JSON.
func TestAppLogCursor_DecodeInvalidJSON(t *testing.T) {
	c := &appLogCursor{}
	// base64 of "not-json"
	if err := c.decode("bm90LWpzb24="); err == nil {
		t.Error("expected error for invalid JSON in cursor")
	}
}

// ---------------------------------------------------------------------------
// parseAppLogHistoryParams edge case tests
// ---------------------------------------------------------------------------

// TestParseAppLogHistoryParams_Defaults verifies default parameter values.
func TestParseAppLogHistoryParams_Defaults(t *testing.T) {
	q := url.Values{}
	p := parseAppLogHistoryParams(q)

	if p.page != 1 {
		t.Errorf("expected default page=1, got %d", p.page)
	}
	if p.perPage != 20 {
		t.Errorf("expected default perPage=20, got %d", p.perPage)
	}
	if p.sortCol != "created_at" {
		t.Errorf("expected default sortCol='created_at', got %q", p.sortCol)
	}
	if p.sortDir != "DESC" {
		t.Errorf("expected default sortDir='DESC', got %q", p.sortDir)
	}
}

// TestParseAppLogHistoryParams_InvalidPageNumber verifies that non-numeric
// page values are ignored (defaults to 1).
func TestParseAppLogHistoryParams_InvalidPageNumber(t *testing.T) {
	q := url.Values{"page": {"abc"}}
	p := parseAppLogHistoryParams(q)

	if p.page != 1 {
		t.Errorf("expected page=1 for invalid input, got %d", p.page)
	}
}

// TestParseAppLogHistoryParams_NegativePageNumber verifies that negative
// page values are ignored (defaults to 1).
func TestParseAppLogHistoryParams_NegativePageNumber(t *testing.T) {
	q := url.Values{"page": {"-1"}}
	p := parseAppLogHistoryParams(q)

	if p.page != 1 {
		t.Errorf("expected page=1 for negative input, got %d", p.page)
	}
}

// TestParseAppLogHistoryParams_ZeroPageNumber verifies that zero
// page values are ignored (defaults to 1).
func TestParseAppLogHistoryParams_ZeroPageNumber(t *testing.T) {
	q := url.Values{"page": {"0"}}
	p := parseAppLogHistoryParams(q)

	if p.page != 1 {
		t.Errorf("expected page=1 for zero input, got %d", p.page)
	}
}

// TestParseAppLogHistoryParams_PerPageClamping verifies that per_page must be
// in [1, 100]. Values outside this range are ignored (defaults to 20).
func TestParseAppLogHistoryParams_PerPageClamping(t *testing.T) {
	cases := []struct {
		name     string
		perPage  string
		expected int
	}{
		{"zero falls back to default", "0", 20},
		{"negative falls back to default", "-5", 20},
		{"over 100 falls back to default", "200", 20},
		{"valid", "50", 50},
		{"invalid string falls back to default", "abc", 20},
		{"boundary 1", "1", 1},
		{"boundary 100", "100", 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{"per_page": {tc.perPage}}
			p := parseAppLogHistoryParams(q)
			if p.perPage != tc.expected {
				t.Errorf("expected perPage=%d, got %d", tc.expected, p.perPage)
			}
		})
	}
}
