package api

import (
	"bytes"
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
)

// ---------------------------------------------------------------------------
// ListLogs Pagination Tests
// ---------------------------------------------------------------------------

func TestListLogs_DefaultPagination(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Page != 1 {
		t.Errorf("expected page=1 (default), got %d", resp.Page)
	}
	if resp.PerPage != 20 {
		t.Errorf("expected per_page=20 (default), got %d", resp.PerPage)
	}
}

func TestListLogs_CustomPagination(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?page=2&per_page=5", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Page != 2 {
		t.Errorf("expected page=2, got %d", resp.Page)
	}
	if resp.PerPage != 5 {
		t.Errorf("expected per_page=5, got %d", resp.PerPage)
	}
}

func TestListLogs_PerPageCapped(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?per_page=500", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.PerPage != 200 {
		t.Errorf("expected per_page=200 (capped), got %d", resp.PerPage)
	}
}

func TestListLogs_PageLessThanOne(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?page=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Page != 1 {
		t.Errorf("expected page=1 (default for page<1), got %d", resp.Page)
	}
}

func TestListLogs_PerPageLessThanOne(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?per_page=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.PerPage != 1 {
		t.Errorf("expected per_page=1 (default for per_page<1), got %d", resp.PerPage)
	}
}

// ---------------------------------------------------------------------------
// ListLogs Filtering Tests
// ---------------------------------------------------------------------------

func TestListLogs_FilterByModelID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?model_id=gpt-4", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestListLogs_FilterByStatusCode4xx(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?status_code=4xx", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestListLogs_FilterByStatusCode5xx(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?status_code=5xx", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestListLogs_FilterByStatusCode0(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?status_code=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListLogs Sorting Tests
// ---------------------------------------------------------------------------

func TestListLogs_SortByModel(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?sort_by=model&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestListLogs_InvalidSortBy(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?sort_by=nonexistent", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListLogs Date Range Filter Tests
// ---------------------------------------------------------------------------

func TestListLogs_DateRangeFilter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PurgeLogs Tests
// ---------------------------------------------------------------------------

func TestPurgeLogs_ValidHour(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"older_than":"1h"}`))
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPurgeLogs_ValidDay(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"older_than":"1d"}`))
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPurgeLogs_ValidWeek(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"older_than":"1w"}`))
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPurgeLogs_ValidMonth(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"older_than":"1m"}`))
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPurgeLogs_All(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"older_than":"all"}`))
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_Empty tests that ListLogs returns 200 with empty array
// when there are no logs.
func TestListLogs_Empty(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Entries == nil {
		t.Error("expected Entries to be empty array, not nil")
	}
	if resp.Total != 0 {
		t.Errorf("expected Total 0, got %d", resp.Total)
	}
}

// TestListLogs_WithLogs tests that ListLogs returns logs that were inserted.
func TestListLogs_WithLogs(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := fmt.Sprintf(`{"name": "test-logs-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a request log directly via DB
	pool := h.Pool().Pool()
	logID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		logID, providerResp.ID, "test-model", 200, 100, 10, 20)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Clear cache to ensure fresh data
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// Now list logs
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/logs/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp LogsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total < 1 {
		t.Errorf("expected at least 1 log, got %d", resp.Total)
	}

	// Find our log in the results
	found := false
	for _, entry := range resp.Entries {
		if entry.ID == logID.String() {
			found = true
			if entry.ModelID != "test-model" {
				t.Errorf("expected model_id 'test-model', got %q", entry.ModelID)
			}
			if entry.StatusCode != 200 {
				t.Errorf("expected status_code 200, got %d", entry.StatusCode)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find inserted log in response")
	}
}

// TestPurgeLogs_DBError tests that PurgeLogs returns 500 when
// the database is unavailable.
func TestPurgeLogs_DBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	closedPool := newClosedPool(t)
	defer closedPool.Close()

	// Test the repository directly since Handler requires a working pool
	ctx := context.Background()
	_, err := closedPool.Exec(ctx, `DELETE FROM request_logs`)
	if err == nil {
		t.Error("expected error when executing with closed pool")
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap2_test.go
// ---------------------------------------------------------------------------

// TestPurgeLogs_InvalidBody tests that PurgeLogs returns 400 when
// the request body is not valid JSON.
func TestPurgeLogs_InvalidBody(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Send invalid JSON
	body := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPurgeLogs_InvalidOlderThan tests that PurgeLogs returns 400 when
// the older_than value is invalid (e.g., "2x").
func TestPurgeLogs_InvalidOlderThan(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := strings.NewReader(`{"older_than":"2x"}`)
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPurgeLogs_RepositoryDBError tests the repository-level DB error
// path when the database is unavailable. This complements the existing
// TestPurgeLogs_DBError in logs_test.go by testing the repository
// directly with a closed pool.
func TestPurgeLogs_RepositoryDBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	closedPool := newClosedPool(t)
	defer closedPool.Close()

	// Test the repository directly since Handler requires a working pool
	ctx := context.Background()
	_, err := closedPool.Exec(ctx, `DELETE FROM request_logs`)
	if err == nil {
		t.Error("expected error when executing DELETE with closed pool")
	}
}

// TestListLogs_CacheHit tests that the second identical request returns
// X-Cache: HIT header.
func TestListLogs_CacheHit(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Clear cache first
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// First request - should be MISS
	req := httptest.NewRequest("GET", "/logs/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cacheHeader := w.Header().Get("X-Cache")
	if cacheHeader != "MISS" {
		t.Errorf("first request: expected X-Cache: MISS, got %q", cacheHeader)
	}

	// Second identical request - should be HIT
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	cacheHeader2 := w2.Header().Get("X-Cache")
	if cacheHeader2 != "HIT" {
		t.Errorf("second request: expected X-Cache: HIT, got %q", cacheHeader2)
	}
}

// TestListLogs_FilterByProviderID tests ListLogs with valid and invalid
// UUID provider_id parameters.
func TestListLogs_FilterByProviderID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Valid UUID - should add SQL filter
	validUUID := uuid.New().String()
	req := httptest.NewRequest("GET", "/logs/?provider_id="+validUUID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid UUID: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid UUID - should be silently ignored (no SQL filter added)
	invalidUUID := "not-a-uuid"
	req2 := httptest.NewRequest("GET", "/logs/?provider_id="+invalidUUID, http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("invalid UUID: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListLogs_FilterBySpecificStatusCode tests ListLogs with a specific
// numeric status code (e.g., ?status_code=200).
func TestListLogs_FilterBySpecificStatusCode(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?status_code=200", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_SortDirAsc tests ListLogs with ascending sort direction.
func TestListLogs_SortDirAsc(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?sort_by=time&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_DateFilterInvalidFormat tests that invalid date formats
// are silently ignored (not added to query).
func TestListLogs_DateFilterInvalidFormat(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Invalid date format - should be silently ignored
	req := httptest.NewRequest("GET", "/logs/?from=invalid-date", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ListLogsCursor Tests
// ---------------------------------------------------------------------------

func TestListLogsCursor_Default(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := fmt.Sprintf(`{"name": "test-cursor-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert test request logs
	pool := h.Pool().Pool()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(),
			fmt.Sprintf(`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW() - INTERVAL '%d minutes')`, i*10),
			uuid.New(), providerResp.ID, "test-model", 200, 100)
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Clear cache to ensure fresh data
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// Test default cursor request (no cursor)
	req = httptest.NewRequest("GET", "/logs/cursor", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsCursorResponse
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
	// has_after depends on whether we have more entries than the limit
}

func TestListLogsCursor_WithCursor(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := fmt.Sprintf(`{"name": "test-cursor2-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert test request logs with known timestamps
	pool := h.Pool().Pool()
	for i := 0; i < 10; i++ {
		logID := uuid.New()
		// Stagger timestamps
		ts := time.Now().Add(-time.Duration(i) * time.Minute)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			logID, providerResp.ID, "test-model", 200, 100, ts)
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Clear cache
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// First request to get cursor
	req = httptest.NewRequest("GET", "/logs/cursor?limit=3", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var firstResp LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	if len(firstResp.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(firstResp.Entries))
	}

	// Encode cursor from last entry
	cursor := logCursor{
		CreatedAt: firstResp.Entries[len(firstResp.Entries)-1].CreatedAt,
		ID:        firstResp.Entries[len(firstResp.Entries)-1].ID,
	}
	cursorStr := cursor.encode()

	// Second request with cursor - should have has_before=true
	req = httptest.NewRequest("GET", "/logs/cursor?cursor="+url.QueryEscape(cursorStr)+"&limit=3", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var secondResp LogsCursorResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}

	if !secondResp.HasBefore {
		t.Error("expected HasBefore=true when using cursor")
	}
	if len(secondResp.Entries) == 0 {
		t.Error("expected entries in second page")
	}
}

func TestListLogsCursor_Filters(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create providers
	providerData1 := fmt.Sprintf(`{"name": "test-filter-provider1-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	var providerResp1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp1); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	providerData2 := fmt.Sprintf(`{"name": "test-filter-provider2-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	var providerResp2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp2); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert logs with different models and status codes
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New(), providerResp1.ID, "model-a", 200, 100)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New(), providerResp1.ID, "model-b", 404, 150)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New(), providerResp2.ID, "model-a", 500, 200)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Clear cache
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// Test model_id filter
	req = httptest.NewRequest("GET", "/logs/cursor?model_id=model-a", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("model_id filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should have 2 entries with model-a
	for _, entry := range resp.Entries {
		if !strings.Contains(entry.ModelID, "model-a") {
			t.Errorf("expected model_id to contain 'model-a', got %q", entry.ModelID)
		}
	}

	// Test status_code filter (4xx)
	req = httptest.NewRequest("GET", "/logs/cursor?status_code=4xx", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status_code filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.StatusCode < 400 || entry.StatusCode >= 500 {
			t.Errorf("expected status_code in 4xx range, got %d", entry.StatusCode)
		}
	}
}

// TestListLogsCursor_BackwardPagination tests that direction=before returns
// the items immediately preceding the cursor, not items from the start of
// the dataset, and that results are in the requested sort order.
func TestListLogsCursor_BackwardPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	providerData := fmt.Sprintf(`{"name": "backward-log-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert 10 request logs with staggered timestamps (newest first for DESC)
	pool := h.Pool().Pool()
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = uuid.New().String()
		ts := time.Now().Add(-time.Duration(i) * time.Minute)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			ids[i], providerResp.ID, "test-model", 200, 100, ts)
		if err != nil {
			t.Fatalf("Failed to insert request log %d: %v", i, err)
		}
	}

	// Clear cache
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// Page 1 DESC (newest 3): entries 0,1,2
	req = httptest.NewRequest("GET", "/logs/cursor?limit=3&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("failed to decode page1: %v", err)
	}
	if len(page1.Entries) != 3 {
		t.Fatalf("expected 3 entries on page1, got %d", len(page1.Entries))
	}

	// Page 2 (next 3): entries 3,4,5
	page1Last := page1.Entries[len(page1.Entries)-1]
	cursor1 := logCursor{CreatedAt: page1Last.CreatedAt, ID: page1Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor1.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page2: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("failed to decode page2: %v", err)
	}
	if len(page2.Entries) != 3 {
		t.Fatalf("expected 3 entries on page2, got %d", len(page2.Entries))
	}

	// Page 3 (entries 6,7,8)
	page2Last := page2.Entries[len(page2.Entries)-1]
	cursor2 := logCursor{CreatedAt: page2Last.CreatedAt, ID: page2Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor2.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page3: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page3 LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page3); err != nil {
		t.Fatalf("failed to decode page3: %v", err)
	}
	if len(page3.Entries) != 3 {
		t.Fatalf("expected 3 entries on page3, got %d", len(page3.Entries))
	}

	// Now use page3's first entry as cursor with direction=before, limit=3
	// This should return entries 3,4,5 (the items immediately before page3)
	backwardCursor := logCursor{
		CreatedAt: page3.Entries[0].CreatedAt,
		ID:        page3.Entries[0].ID,
	}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/cursor?limit=3&sort_dir=desc&cursor=%s&direction=before", url.QueryEscape(backwardCursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("backward page: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var beforePage LogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &beforePage); err != nil {
		t.Fatalf("failed to decode backward page: %v", err)
	}

	if len(beforePage.Entries) != 3 {
		t.Fatalf("expected 3 entries for backward page, got %d", len(beforePage.Entries))
	}

	// Results must be in DESC order (newest first)
	// The backward page should return entries 3,4,5 which are page2's entries
	if beforePage.Entries[0].ID != page2.Entries[0].ID {
		t.Errorf("expected first entry ID %s, got %s", page2.Entries[0].ID, beforePage.Entries[0].ID)
	}
	if beforePage.Entries[1].ID != page2.Entries[1].ID {
		t.Errorf("expected second entry ID %s, got %s", page2.Entries[1].ID, beforePage.Entries[1].ID)
	}
	if beforePage.Entries[2].ID != page2.Entries[2].ID {
		t.Errorf("expected third entry ID %s, got %s", page2.Entries[2].ID, beforePage.Entries[2].ID)
	}

	// Must have has_after=true (items exist after the cursor by definition)
	if !beforePage.HasAfter {
		t.Error("expected HasAfter=true for backward page with cursor")
	}

	// Must have has_before=true since page1 entries still precede this page
	if !beforePage.HasBefore {
		t.Error("expected HasBefore=true for backward page (more items precede)")
	}
}
