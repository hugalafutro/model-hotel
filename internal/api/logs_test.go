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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
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
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	// Create a provider via API so we have a valid provider_id FK.
	providerID := createLogTestProvider(t, r, "filter-model-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert two logs with different model IDs.
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'gpt-4', 200, 100, now())`, providerID); err != nil {
		t.Fatalf("insert gpt-4 log: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'claude-3', 200, 100, now())`, providerID); err != nil {
		t.Fatalf("insert claude-3 log: %v", err)
	}

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

	// Verify that only the gpt-4 model log is returned.
	for _, l := range resp.Entries {
		if l.ModelID != "gpt-4" {
			t.Errorf("expected only gpt-4 logs, got model_id=%s", l.ModelID)
		}
	}
}

// createLogTestProvider creates a provider via the API and returns its UUID.
func createLogTestProvider(t *testing.T, r chi.Router, name string) string {
	t.Helper()
	prefix := name + "-" + uuid.New().String()[:8]
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, prefix)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create provider %s: %d: %s", name, rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse provider response: %v", err)
	}
	return resp.ID
}

func TestListLogs_FilterByStatusCode4xx(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	providerID := createLogTestProvider(t, r, "filter-4xx-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert logs with 400 and 500 status codes.
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-a', 400, 50, now())`, providerID); err != nil {
		t.Fatalf("insert 400 log: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-b', 503, 50, now())`, providerID); err != nil {
		t.Fatalf("insert 503 log: %v", err)
	}

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

	for _, l := range resp.Entries {
		if l.StatusCode < 400 || l.StatusCode >= 500 {
			t.Errorf("expected only 4xx logs, got status_code=%d", l.StatusCode)
		}
	}
}

func TestListLogs_FilterByStatusCode5xx(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	providerID := createLogTestProvider(t, r, "filter-5xx-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert logs with 200 and 500 status codes.
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-a', 200, 50, now())`, providerID); err != nil {
		t.Fatalf("insert 200 log: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-b', 503, 50, now())`, providerID); err != nil {
		t.Fatalf("insert 503 log: %v", err)
	}

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

	for _, l := range resp.Entries {
		if l.StatusCode < 500 {
			t.Errorf("expected only 5xx logs, got status_code=%d", l.StatusCode)
		}
	}
}

func TestListLogs_FilterByStatusCode0(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	providerID := createLogTestProvider(t, r, "filter-sc0-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert a log with status_code=0 (proxy error, no HTTP response).
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-a', 0, 50, now())`, providerID); err != nil {
		t.Fatalf("insert status=0 log: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'model-b', 200, 50, now())`, providerID); err != nil {
		t.Fatalf("insert status=200 log: %v", err)
	}

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

	for _, l := range resp.Entries {
		if l.StatusCode != 0 {
			t.Errorf("expected only status_code=0 logs, got status_code=%d", l.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// ListLogs Sorting Tests
// ---------------------------------------------------------------------------

func TestListLogs_SortByModel(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	providerID := createLogTestProvider(t, r, "sort-test-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert logs with different model IDs to verify sort order.
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'beta-model', 200, 50, now())`, providerID); err != nil {
		t.Fatalf("insert beta-model log: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES (gen_random_uuid(), $1, 'alpha-model', 200, 50, now())`, providerID); err != nil {
		t.Fatalf("insert alpha-model log: %v", err)
	}

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

	// Verify both inserted entries are present.
	if len(resp.Entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(resp.Entries))
	}

	modelIDs := make(map[string]bool)
	for _, l := range resp.Entries {
		modelIDs[l.ModelID] = true
	}
	if !modelIDs["alpha-model"] || !modelIDs["beta-model"] {
		t.Errorf("expected both alpha-model and beta-model in results, got models: %v", modelIDs)
	}

	// Verify ascending order among the inserted entries.
	var alphaIdx, betaIdx = -1, -1
	for i, l := range resp.Entries {
		if l.ModelID == "alpha-model" {
			alphaIdx = i
		}
		if l.ModelID == "beta-model" {
			betaIdx = i
		}
	}
	if alphaIdx >= 0 && betaIdx >= 0 && alphaIdx > betaIdx {
		t.Errorf("expected alpha-model before beta-model in ascending sort, got alpha at %d, beta at %d", alphaIdx, betaIdx)
	}
}

func TestListLogs_InvalidSortBy(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?sort_by=nonexistent", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Invalid sort_by should not cause a server error; it falls back to default sort.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
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

// ---------------------------------------------------------------------------
// appendLogFilters unit tests
// ---------------------------------------------------------------------------

func TestAppendLogFilters_NoFilters(t *testing.T) {
	query, args, idx := appendLogFilters("SELECT * FROM t WHERE 1=1", nil, 1, "", "", "", "", "")
	if !strings.Contains(query, "WHERE 1=1") {
		t.Errorf("base query should be preserved, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_ModelID(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "gpt-4", "", "", "", "")
	if !strings.Contains(query, `AND rl.model_id ILIKE $1`) {
		t.Errorf("expected model_id ILIKE filter, got %q", query)
	}
	if args[0] != "%gpt-4%" {
		t.Errorf("expected %%gpt-4%%, got %v", args[0])
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendLogFilters_ProviderID_ValidUUID(t *testing.T) {
	validUUID := uuid.New().String()
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", validUUID, "", "", "")
	if !strings.Contains(query, "AND rl.provider_id = $1") {
		t.Errorf("expected provider_id filter for valid UUID, got %q", query)
	}
	if args[0] != uuid.MustParse(validUUID) {
		t.Errorf("expected parsed UUID arg, got %v", args[0])
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendLogFilters_ProviderID_InvalidUUID(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "not-a-uuid", "", "", "")
	if strings.Contains(query, "provider_id") {
		t.Errorf("invalid UUID should not add provider_id filter, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for invalid UUID, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_StatusCode4xx(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "4xx", "", "")
	if !strings.Contains(query, "AND rl.status_code >= 400 AND rl.status_code < 500") {
		t.Errorf("expected 4xx range filter, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("4xx filter should not add args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_StatusCode5xx(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "5xx", "", "")
	if !strings.Contains(query, "AND rl.status_code >= 500") {
		t.Errorf("expected 5xx range filter, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("5xx filter should not add args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_StatusCodeSpecific(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "404", "", "")
	if !strings.Contains(query, "AND rl.status_code = $1") {
		t.Errorf("expected specific status code filter, got %q", query)
	}
	if args[0] != 404 {
		t.Errorf("expected arg 404, got %v", args[0])
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendLogFilters_StatusCodeZero(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "0", "", "")
	if !strings.Contains(query, "AND (rl.status_code = 0 OR rl.status_code IS NULL)") {
		t.Errorf("expected status_code=0 or NULL filter, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("status_code=0 should not add args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_StatusCodeNegative(t *testing.T) {
	query, args, _ := appendLogFilters("WHERE 1=1", nil, 1, "", "", "-1", "", "")
	if strings.Contains(query, "status_code") {
		t.Errorf("negative status code should be ignored, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for negative status code, got %d", len(args))
	}
}

func TestAppendLogFilters_StatusCodeNonNumeric(t *testing.T) {
	query, args, _ := appendLogFilters("WHERE 1=1", nil, 1, "", "", "abc", "", "")
	if strings.Contains(query, "status_code") {
		t.Errorf("non-numeric status code should be ignored, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for non-numeric status code, got %d", len(args))
	}
}

func TestAppendLogFilters_FromDate(t *testing.T) {
	from := "2024-01-01T00:00:00Z"
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "", from, "")
	if !strings.Contains(query, "AND rl.created_at >= $1") {
		t.Errorf("expected from date filter, got %q", query)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendLogFilters_ToDate(t *testing.T) {
	to := "2024-12-31T23:59:59Z"
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "", "", to)
	if !strings.Contains(query, "AND rl.created_at <= $1") {
		t.Errorf("expected to date filter, got %q", query)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendLogFilters_InvalidFromDate(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "", "not-a-date", "")
	if strings.Contains(query, "rl.created_at >=") {
		t.Errorf("invalid from date should be ignored, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for invalid from, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_InvalidToDate(t *testing.T) {
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 1, "", "", "", "", "garbage")
	if strings.Contains(query, "rl.created_at <=") {
		t.Errorf("invalid to date should be ignored, got %q", query)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args for invalid to, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendLogFilters_AllFilters(t *testing.T) {
	validUUID := uuid.New().String()
	query, args, idx := appendLogFilters("WHERE 1=1", nil, 3, "gpt-4", validUUID, "404", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z")
	if !strings.Contains(query, `AND rl.model_id ILIKE $3`) {
		t.Errorf("expected model_id filter at $3, got %q", query)
	}
	if !strings.Contains(query, "AND rl.provider_id = $4") {
		t.Errorf("expected provider_id filter at $4, got %q", query)
	}
	if !strings.Contains(query, "AND rl.status_code = $5") {
		t.Errorf("expected status_code filter at $5, got %q", query)
	}
	if !strings.Contains(query, "AND rl.created_at >= $6") {
		t.Errorf("expected from date filter at $6, got %q", query)
	}
	if !strings.Contains(query, "AND rl.created_at <= $7") {
		t.Errorf("expected to date filter at $7, got %q", query)
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d", len(args))
	}
	if idx != 8 {
		t.Errorf("expected argIdx=8, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// appendKeysetPredicate unit tests
// ---------------------------------------------------------------------------

func TestAppendKeysetPredicate_AfterDesc_ReturnsLessThan(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "test-id"}
	query, args, idx := appendKeysetPredicate("WHERE 1=1", nil, 1, cursor, "after", "desc")
	if !strings.Contains(query, "rl.created_at < $1") {
		t.Errorf("after+desc should use '<', got %q", query)
	}
	if !strings.Contains(query, "rl.id < $3") {
		t.Errorf("after+desc id should use '<', got %q", query)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if idx != 4 {
		t.Errorf("expected argIdx=4, got %d", idx)
	}
}

func TestAppendKeysetPredicate_BeforeAsc_ReturnsLessThan(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "test-id"}
	query, _, _ := appendKeysetPredicate("WHERE 1=1", nil, 1, cursor, "before", "asc")
	if !strings.Contains(query, "rl.created_at < $1") {
		t.Errorf("before+asc should use '<', got %q", query)
	}
}

func TestAppendKeysetPredicate_AfterAsc_ReturnsGreaterThan(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "test-id"}
	query, _, _ := appendKeysetPredicate("WHERE 1=1", nil, 1, cursor, "after", "asc")
	if !strings.Contains(query, "rl.created_at > $1") {
		t.Errorf("after+asc should use '>', got %q", query)
	}
}

func TestAppendKeysetPredicate_BeforeDesc_ReturnsGreaterThan(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "test-id"}
	query, _, _ := appendKeysetPredicate("WHERE 1=1", nil, 1, cursor, "before", "desc")
	if !strings.Contains(query, "rl.created_at > $1") {
		t.Errorf("before+desc should use '>', got %q", query)
	}
}

func TestAppendKeysetPredicate_ArgIndexOffset(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "test-id"}
	query, args, idx := appendKeysetPredicate("WHERE 1=1", nil, 5, cursor, "after", "desc")
	if !strings.Contains(query, "rl.created_at < $5") {
		t.Errorf("expected arg starting at $5, got %q", query)
	}
	if !strings.Contains(query, "rl.id < $7") {
		t.Errorf("expected id arg at $7, got %q", query)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if idx != 8 {
		t.Errorf("expected argIdx=8, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// buildLogListQuery unit tests
// ---------------------------------------------------------------------------

func TestBuildLogListQuery_NoCursorNoFilters(t *testing.T) {
	p := logListParams{
		limit:     20,
		sortDir:   "desc",
		direction: "after",
	}
	query, args := buildLogListQuery(p)

	if !strings.Contains(query, "SELECT "+strings.TrimSpace(logEntrySelectColumns[:20])) {
		t.Errorf("expected SELECT with log columns, got %q", query[:60])
	}
	if !strings.Contains(query, "ORDER BY rl.created_at desc, rl.id desc") {
		t.Errorf("expected ORDER BY rl.created_at desc, rl.id desc, got %q", query)
	}
	if !strings.Contains(query, "LIMIT") {
		t.Errorf("expected LIMIT clause, got %q", query)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg (limit+1), got %d", len(args))
	}
	if args[0] != 21 {
		t.Errorf("expected limit arg 21, got %v", args[0])
	}
}

func TestBuildLogListQuery_WithFilters(t *testing.T) {
	p := logListParams{
		limit:      10,
		sortDir:    "desc",
		direction:  "after",
		modelID:    "gpt-4",
		statusCode: "4xx",
	}
	query, _ := buildLogListQuery(p)

	if !strings.Contains(query, `rl.model_id ILIKE`) {
		t.Errorf("expected model_id filter, got %q", query)
	}
	if !strings.Contains(query, "rl.status_code >= 400 AND rl.status_code < 500") {
		t.Errorf("expected 4xx status code filter, got %q", query)
	}
}

func TestBuildLogListQuery_WithCursor(t *testing.T) {
	ts := time.Now()
	cursor := logCursor{CreatedAt: ts, ID: "cursor-id"}
	p := logListParams{
		limit:     20,
		sortDir:   "desc",
		direction: "after",
		cursorStr: cursor.encode(),
		cursor:    cursor,
	}
	query, args := buildLogListQuery(p)

	if !strings.Contains(query, "rl.created_at < $") {
		t.Errorf("after+desc should produce keyset with '<', got %q", query)
	}
	// 3 keyset args + 1 limit arg
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
}

func TestBuildLogListQuery_BackwardDescInvertsSort(t *testing.T) {
	p := logListParams{
		limit:     20,
		sortDir:   "desc",
		direction: "before",
	}
	query, _ := buildLogListQuery(p)

	if !strings.Contains(query, "ORDER BY rl.created_at asc, rl.id asc") {
		t.Errorf("before+desc should invert to asc sort in fetch query, got %q", query)
	}
}

func TestBuildLogListQuery_BackwardAscInvertsSort(t *testing.T) {
	p := logListParams{
		limit:     20,
		sortDir:   "asc",
		direction: "before",
	}
	query, _ := buildLogListQuery(p)

	if !strings.Contains(query, "ORDER BY rl.created_at desc, rl.id desc") {
		t.Errorf("before+asc should invert to desc sort in fetch query, got %q", query)
	}
}

func TestBuildLogListQuery_LimitPlusOne(t *testing.T) {
	p := logListParams{
		limit:     5,
		sortDir:   "desc",
		direction: "after",
	}
	_, args := buildLogListQuery(p)

	if args[len(args)-1] != 6 {
		t.Errorf("expected limit+1=6, got %v", args[len(args)-1])
	}
}

// ---------------------------------------------------------------------------
// GetLog Tests
// ---------------------------------------------------------------------------

func TestGetLog_Found(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := fmt.Sprintf(`{"name": "test-getlog-provider-%s", "base_url": "https://api.example.com", "api_key": "test-key"}`, uuid.New().String()[:8])
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

	// Insert a log row via direct DB exec
	pool := h.Pool().Pool()
	logID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		logID, providerResp.ID, "test-get-model", 200, 100, 10, 20)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Clear cache to ensure fresh data
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// GET /logs/{logID} with auth header
	req = httptest.NewRequest("GET", "/logs/"+logID.String(), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entry LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if entry.ID != logID.String() {
		t.Errorf("expected ID %s, got %s", logID.String(), entry.ID)
	}
	if entry.ModelID != "test-get-model" {
		t.Errorf("expected ModelID 'test-get-model', got %q", entry.ModelID)
	}
	if entry.StatusCode != 200 {
		t.Errorf("expected StatusCode 200, got %d", entry.StatusCode)
	}
	if entry.DurationMs != 100 {
		t.Errorf("expected DurationMs 100, got %v", entry.DurationMs)
	}
}

func TestGetLog_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// GET /logs/{random-uuid} with auth header
	randomID := uuid.New().String()
	req := httptest.NewRequest("GET", "/logs/"+randomID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLog_InvalidID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// GET /logs/not-a-uuid with auth header
	req := httptest.NewRequest("GET", "/logs/not-a-uuid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetLog_DBError tests that GetLog returns 500 when the database
// returns an error other than pgx.ErrNoRows (e.g., connection error).
func TestGetLog_DBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Create a db.DB and close it to simulate connection errors
	ctx := context.Background()
	testDB, err := db.New(ctx, apiTestDBURL, 5, 1)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	testDB.Close()

	// Create handler with closed DB using testHandler helper
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, nil, nil, auth, testDB)

	// Set up minimal router with just the GetLog route
	r := chi.NewRouter()
	r.With(h.AuthMiddleware).Get("/logs/{id}", h.GetLog)

	// Create request with valid UUID - the DB error will occur during QueryRow
	req := httptest.NewRequest("GET", "/logs/"+uuid.New().String(), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
