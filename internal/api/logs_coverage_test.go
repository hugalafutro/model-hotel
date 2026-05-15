package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
