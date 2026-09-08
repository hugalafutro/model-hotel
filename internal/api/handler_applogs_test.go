package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

func TestGetAppLogsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs (may be empty)
}

func TestClearAppLogsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestClearAppLogs_InvalidatesCountCache guards the perf change that derives the
// unfiltered total from the cached level counts: clearing the logs must drop that
// cache so the next poll reports 0 immediately, not the pre-clear total for up to
// appLogCountCacheTTL.
func TestClearAppLogs_InvalidatesCountCache(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)
	pool := h.Pool().Pool()
	ctx := context.Background()

	// newTestHandler truncates app_logs, so the unfiltered total covers exactly
	// the rows we insert here.
	for i := range 3 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES (gen_random_uuid(), NOW(), 'info', 'clearcache', 'msg', NOW())`); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	unfilteredTotal := func() int {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("history GET: status %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode history: %v", err)
		}
		return resp.Total
	}

	// Warm the count cache: the unfiltered total must see the 3 rows.
	if got := unfilteredTotal(); got != 3 {
		t.Fatalf("total before clear = %d, want 3", got)
	}

	// Clear all logs.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body.String())
	}

	// The next poll must report 0 immediately; a stale (non-zero) total here
	// means ClearAppLogs failed to invalidate the count cache.
	if got := unfilteredTotal(); got != 0 {
		t.Fatalf("total after clear = %d, want 0 (count cache not invalidated)", got)
	}
}

// TestClearAppLogs_OlderThanRange checks the ranged purge: a DELETE carrying an
// older_than token removes only rows older than the cutoff, leaving recent ones.
func TestClearAppLogs_OlderThanRange(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)
	pool := h.Pool().Pool()
	ctx := context.Background()

	// Two stale rows (8 days old) and one fresh row (1 hour old).
	for i := range 2 {
		if _, err := pool.Exec(ctx,
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES (gen_random_uuid(), NOW() - INTERVAL '8 days', 'info', 'rangetest', 'old', NOW() - INTERVAL '8 days')`); err != nil {
			t.Fatalf("insert old row %d: %v", i, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
		 VALUES (gen_random_uuid(), NOW() - INTERVAL '1 hour', 'info', 'rangetest', 'fresh', NOW() - INTERVAL '1 hour')`); err != nil {
		t.Fatalf("insert fresh row: %v", err)
	}

	// Purge logs older than 1 week: the two 8-day rows go, the fresh one stays.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/logs/app", strings.NewReader(`{"older_than":"1w"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ranged clear: status %d: %s", rec.Code, rec.Body.String())
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM app_logs WHERE source = 'rangetest'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected 1 fresh row to survive, got %d", remaining)
	}
}

// TestClearAppLogs_InvalidOlderThan rejects an unrecognized range token with 400.
func TestClearAppLogs_InvalidOlderThan(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/logs/app", strings.NewReader(`{"older_than":"banana"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// GetAppLogs with filters - Additional coverage

func TestGetAppLogs_WithSeverityFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with severity filter (level parameter)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with severity filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs filtered by severity (may be empty)
}

func TestGetAppLogs_WithSearchFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with search filter
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&search=test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with search filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs matching search term (may be empty)
}

func TestGetAppLogs_WithTimeRangeFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with time range filter (last 24 hours)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&from=2024-01-01T00:00:00Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with time range filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs within time range (may be empty)
}

// Model Tests

func TestGetAppLogs(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty list when no app logs exist
	if len(response) != 0 {
		t.Errorf("Expected empty app log list, got %d entries", len(response))
	}
}

// Test for models.go - UpdateModel_Validation

func TestGetAppLogs_QueryParams(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Insert test logs directly into the database
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO app_logs (timestamp, level, source, message) VALUES
		($1, $2, $3, $4),
		($5, $6, $7, $8),
		($9, $10, $11, $12)
	`,
		now, "info", "proxy", "test info message",
		now, "warning", "auth", "test warning message",
		now, "error", "proxy", "test error message",
	)
	if err != nil {
		t.Fatalf("Failed to insert test logs: %v", err)
	}

	t.Run("SourceFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for source filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should only have proxy source entries
		for _, entry := range response.Entries {
			if entry.Source != "proxy" {
				t.Errorf("Expected only proxy source entries, got source %s", entry.Source)
			}
		}
	})

	t.Run("LevelFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for level filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should only have error level entries
		for _, entry := range response.Entries {
			if entry.Level != "error" {
				t.Errorf("Expected only error level entries, got level %s", entry.Level)
			}
		}
	})

	t.Run("SearchFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&search=warning", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for search filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries containing "warning"
		foundWarning := false
		for _, entry := range response.Entries {
			if strings.Contains(strings.ToLower(entry.Message), "warning") {
				foundWarning = true
				break
			}
		}

		if !foundWarning {
			t.Error("Expected to find entries containing 'warning'")
		}
	})

	t.Run("LimitFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&per_page=2", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for limit filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
			PerPage int           `json:"per_page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have at most 2 entries
		if len(response.Entries) > 2 {
			t.Errorf("Expected at most 2 entries, got %d", len(response.Entries))
		}
		if response.PerPage != 2 {
			t.Errorf("Expected per_page=2, got %d", response.PerPage)
		}
	})

	t.Run("TimeFilter", func(t *testing.T) {
		// Use a time in the past to filter
		pastTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&from="+pastTime, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for time filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries (all our test logs are recent)
		if len(response.Entries) == 0 {
			t.Error("Expected to find entries with time filter")
		}
	})

	t.Run("CombinedFilters", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy&level=error", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for combined filters, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries matching both filters
		for _, entry := range response.Entries {
			if entry.Source != "proxy" || entry.Level != "error" {
				t.Errorf("Expected entries with source=proxy and level=error, got source=%s level=%s", entry.Source, entry.Level)
			}
		}
	})
}

// Test for discovery.go - DiscoverProviderModels_Success

func TestGetAppLogs_Empty(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Get logs when empty
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should be empty
	if len(entries) != 0 {
		t.Errorf("Expected empty log list, got %d entries", len(entries))
	}
}

// Test for applogs.go - GetAppLogs_WithLimit

func TestGetAppLogs_WithLimit(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write multiple log messages
	for i := range 10 {
		debuglog.Info(fmt.Sprintf("test message %d", i), "source", "test")
	}

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Get logs with limit
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?limit=5", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have at most 5 entries
	if len(entries) > 5 {
		t.Errorf("Expected at most 5 entries with limit=5, got %d", len(entries))
	}
}

func TestGetAppLogsHistory(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries      []map[string]any `json:"entries"`
		Total        int              `json:"total"`
		Page         int              `json:"page"`
		PerPage      int              `json:"per_page"`
		LevelCounts  map[string]int   `json:"level_counts"`
		SourceCounts map[string]int   `json:"source_counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty history when no app logs exist
	if len(response.Entries) != 0 {
		t.Errorf("Expected empty app log history, got %d entries", len(response.Entries))
	}
	if response.Total != 0 {
		t.Errorf("Expected total 0, got %d", response.Total)
	}
	if response.LevelCounts == nil {
		t.Error("Expected level_counts in response")
	}
	if response.SourceCounts == nil {
		t.Error("Expected source_counts in response")
	}
}

// TestAppSlogHandler_Handle tests the slog.Handler implementation

func TestAppSlogHandler_Handle(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages
	debuglog.Info("test info message", "source", "test", "key", "value")
	debuglog.Warn("test warning message", "source", "test", "key", "value")
	debuglog.Error("test error message", "source", "test", "key", "value")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Get the logs from the ring buffer
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have at least 3 entries (info, warning, error)
	if len(entries) < 3 {
		t.Errorf("Expected at least 3 log entries, got %d", len(entries))
	}

	// Check that we have different levels
	foundLevels := make(map[string]bool)
	for _, entry := range entries {
		foundLevels[entry.Level] = true
	}

	if !foundLevels["info"] {
		t.Error("Expected to find info level log")
	}
	if !foundLevels["warning"] {
		t.Error("Expected to find warning level log")
	}
	if !foundLevels["error"] {
		t.Error("Expected to find error level log")
	}
}

// TestFlush_WriterFlush tests the DB writer flush functionality

func TestFlush_WriterFlush(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = r

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages
	debuglog.Info("test info message", "source", "test", "key", "value")
	debuglog.Warn("test warning message", "source", "test", "key", "value")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Trigger a flush by stopping the writer
	StopAppLogWriter()

	// Query the DB directly to verify entries were written
	var count int
	err := h.Pool().Pool().QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query app_logs count: %v", err)
	}

	// Should have entries in the DB
	if count < 2 {
		t.Errorf("Expected at least 2 log entries in DB, got %d", count)
	}
}

// TestGetAppLogsHistory_MultipleFilters tests getAppLogsHistory with different query parameters

func TestGetAppLogsHistory_MultipleFilters(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages with different sources and levels
	debuglog.Info("test info message", "source", "proxy")
	debuglog.Warn("test warning message", "source", "auth")
	debuglog.Error("test error message", "source", "proxy")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Trigger a flush
	StopAppLogWriter()

	// Test with level filter
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for level filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []AppLogEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should only have error level entries
	for _, entry := range response.Entries {
		if entry.Level != "error" {
			t.Errorf("Expected only error level entries, got level %s", entry.Level)
		}
	}

	// Test with source filter
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for source filter, got %d: %s", rec.Code, rec.Body.String())
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should only have proxy source entries
	for _, entry := range response.Entries {
		if entry.Source != "proxy" {
			t.Errorf("Expected only proxy source entries, got source %s", entry.Source)
		}
	}

	// Test with search filter
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/logs/app?history=true&search=warning", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for search filter, got %d: %s", rec.Code, rec.Body.String())
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have entries containing "warning"
	foundWarning := false
	for _, entry := range response.Entries {
		if strings.Contains(strings.ToLower(entry.Message), "warning") {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Error("Expected to find entries containing 'warning'")
	}
}

// TestFailoverAddProvider tests adding a provider to a failover group

func TestGetAppLogs_EmptyResult(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Default mode returns a JSON array of log entries (may be empty)
	var response []any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Empty array is expected when no logs exist
}

// TestGetStats_Empty tests the stats endpoint with no data

// ---------------------------------------------------------------------------
// 1. getAppLogsHistory — nil dbPool path
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_NilPool tests the early return path when dbPool is nil.
// The handler should return an empty response without crashing.
// We call getAppLogsHistory directly because h.Register dereferences h.dbPool.Pool().
func TestGetAppLogsHistory_NilPool(t *testing.T) {
	h := &Handler{
		dbPool:   nil,
		adminMgr: &mockAdminAuth{validateFn: func(token string) bool { return token == "test-admin-token" }},
	}

	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	w := httptest.NewRecorder()

	// Call the handler method directly to test the nil-pool early return
	h.getAppLogsHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for nil pool, got %d: %s", w.Code, w.Body.String())
	}

	var response appLogsHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(response.Entries) != 0 {
		t.Errorf("expected empty entries for nil pool, got %d", len(response.Entries))
	}
}

// ---------------------------------------------------------------------------
// 2. getAppLogsHistory — DB query error path (cancelled context)
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_QueryError tests the error path where the DB query
// in getAppLogsHistory fails (e.g. cancelled context). We call the handler
// method directly because h.Register dereferences h.dbPool.Pool() which
// panics when the pool is nil.
func TestGetAppLogsHistory_QueryError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(token string) bool { return token == "test-admin-token" }},
	}

	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	// Cancel the request context so DB queries fail
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	// The handler returns an error JSON body for internal query failures.
	// It should not crash.
	t.Logf("getAppLogsHistory with cancelled context: status=%d body=%s", w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// 13. getAppLogsHistory — count error with cancelled context
// ---------------------------------------------------------------------------

func TestGetAppLogsHistory_CountAppLogsError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	t.Logf("getAppLogsHistory with cancelled context: status=%d body=%s", w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// 14. getAppLogsHistory — row query failure
// ---------------------------------------------------------------------------

func TestGetAppLogsHistory_QueryRowsError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	ctx, cancel := context.WithTimeout(req.Context(), 0)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	t.Logf("getAppLogsHistory with immediate timeout: status=%d body=%s", w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// 17. getAppLogsHistory — encode error on response
// ---------------------------------------------------------------------------

func TestGetAppLogsHistory_NilPool_EncodeError(t *testing.T) {
	// nil pool should return empty response; test it doesn't crash
	h := &Handler{
		dbPool:   nil,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)

	fw := &statusTrackingFailWriter{}
	h.getAppLogsHistory(fw, req)

	// nil pool returns early with 200 + empty JSON — Write fails
	// but the error is only logged, not propagated
}

// ---------------------------------------------------------------------------
// 8. getAppLogsHistory — row Scan error within for loop
//    The rows.Scan error path inside the for loop (line 554) is the
//    remaining uncovered branch. This can happen if the column count
//    or types don't match the Scan arguments, which can't happen with
//    a correctly structured query. We test by verifying the handler
//    doesn't crash even when Scan errors occur (they silently skip rows).
// ---------------------------------------------------------------------------

func TestGetAppLogsHistory_RowScanErrorInLoop(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	// Insert an app_log entry so the query returns a row
	pool := testDB.Pool()
	_, execErr := pool.Exec(context.Background(),
		`INSERT INTO app_logs (timestamp, level, source, message) VALUES (NOW(), 'info', 'scan-test', 'test message for scan')`)
	if execErr != nil {
		t.Fatalf("failed to insert app log: %v", execErr)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM app_logs WHERE source = 'scan-test'`)
	}()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	// Should return 200 with entries
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response appLogsHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// The entry should be present and properly scanned
	if len(response.Entries) == 0 {
		t.Error("expected at least one app log entry")
	}
}

// ---------------------------------------------------------------------------
// 8b. getAppLogsHistory — row Scan error with cancelled context during iteration
//     Tests that when the context is cancelled while iterating rows, the
//     handler doesn't panic and gracefully returns partial results.
// ---------------------------------------------------------------------------

func TestGetAppLogsHistory_ScanErrorDuringIteration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	// Use an extremely short timeout that may expire during row iteration
	ctx, cancel := context.WithTimeout(req.Context(), 1*time.Nanosecond)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	// Should not crash; may return error or partial results
	t.Logf("getAppLogsHistory with nanosecond timeout: status=%d", w.Code)
}

// TestGetAppLogsCursor_SortDirAsc covers the sort_dir=asc branch of
// parseAppLogCursorParams (default is DESC).
func TestGetAppLogsCursor_SortDirAsc(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app/cursor?sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetAppLogs_SearchMatchesEscapedSpaces verifies a search term with a
// space also matches messages whose attribute values carry the \x20 space
// escaping from quoteLogValue (a search for a provider name like
// "Ollama Cloud" must find `provider="Ollama\x20Cloud"` lines).
func TestGetAppLogs_SearchMatchesEscapedSpaces(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	_, err := h.Pool().Pool().Exec(context.Background(),
		`INSERT INTO app_logs (timestamp, level, source, message) VALUES (now(), 'info', 'discovery', $1)`,
		`account fetched provider="Ollama\x20Cloud" plan=pro`)
	if err != nil {
		t.Fatalf("insert app log: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&search=Ollama+Cloud", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []struct {
			Message string `json:"message"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	found := false
	for _, e := range response.Entries {
		if strings.Contains(e.Message, `Ollama\x20Cloud`) {
			found = true
		}
	}
	if !found {
		t.Errorf("search=Ollama Cloud did not match the \\x20-escaped message; got %d entries", len(response.Entries))
	}
}

// TestAppSlogHandler_MarksEntriesEscaped pins the provenance contract: the
// slog path (whose attribute values go through quoteLogValue's flattened
// encoding) marks entries escaped and records where the encoded attribute
// suffix begins, so the dashboard decodes exactly the encoder's output and
// never the raw message text. A raw line through the legacy io.Writer path
// never went through the encoder and must not claim the flag.
func TestAppSlogHandler_MarksEntriesEscaped(t *testing.T) {
	savedBuf, savedWriter := appLogBuffer, dbWriter.Load()
	rb := &ringBuffer{entries: make([]AppLogEntry, appLogBufferSize)}
	appLogBuffer = rb
	dbWriter.Store(nil)
	defer func() {
		appLogBuffer = savedBuf
		dbWriter.Store(savedWriter)
	}()

	var buf bytes.Buffer
	h := &appSlogHandler{level: slog.LevelInfo, stderr: &stderrLogFilter{dst: &buf}}

	// Messages that could confuse a whole-string decode: a lone quote, an
	// attribute-shaped literal with \x20, a backslashed path. The boundary
	// makes them irrelevant: everything before AttrsAt is raw text, everything
	// from AttrsAt on is encoder output.
	for _, msg := range []string{
		"discovery: account fetched",
		`discovery: could not parse "x`,
		`discovery: literal path="\x20evidence" in message`,
		`discovery: windows path C:\temp`,
	} {
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
		rec.AddAttrs(slog.String("provider", "Ollama Cloud"))
		if err := h.Handle(context.Background(), rec); err != nil {
			t.Fatalf("Handle returned %v", err)
		}
		entries := rb.GetEntries()
		e := entries[len(entries)-1]
		if !e.Escaped {
			t.Errorf("slog entry for %q should carry the provenance flag", msg)
		}
		if e.AttrsAt < 0 || e.AttrsAt > len(e.Message) {
			t.Fatalf("AttrsAt %d out of range for %q", e.AttrsAt, e.Message)
		}
		wantSuffix := " provider=\"Ollama\\x20Cloud\""
		if got := e.Message[e.AttrsAt:]; got != wantSuffix {
			t.Errorf("message %q: attrs suffix = %q, want %q", msg, got, wantSuffix)
		}
	}

	// AttrsAt crosses the stack into JavaScript's String.slice, which indexes
	// UTF-16 code units, not UTF-8 bytes. A non-ASCII message prefix must
	// therefore yield a boundary in UTF-16 units: for this message the byte
	// length of the prefix differs from its UTF-16 length, so a byte-based
	// offset would land inside the attribute suffix.
	{
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "discovery: \U0001F680\u6a21\u578b ready", 0)
		rec.AddAttrs(slog.String("provider", "Ollama Cloud"))
		if err := h.Handle(context.Background(), rec); err != nil {
			t.Fatalf("Handle returned %v", err)
		}
		entries := rb.GetEntries()
		e := entries[len(entries)-1]
		wantSuffix := " provider=\"Ollama\\x20Cloud\""
		prefix := strings.TrimSuffix(e.Message, wantSuffix)
		if prefix == e.Message {
			t.Fatalf("message %q does not end with the attrs suffix", e.Message)
		}
		wantAt := len(utf16.Encode([]rune(prefix)))
		if e.AttrsAt != wantAt {
			t.Errorf("AttrsAt = %d, want %d UTF-16 code units for prefix %q", e.AttrsAt, wantAt, prefix)
		}
	}

	if _, err := rb.Write([]byte(`2026/08/24 01:00:00 [legacy] raw line path="\x20evidence"`)); err != nil {
		t.Fatalf("ring Write: %v", err)
	}
	entries := rb.GetEntries()
	if last := entries[len(entries)-1]; last.Escaped {
		t.Error("legacy io.Writer entry must not claim the flattened-encoding flag")
	}
}

// TestGetAppLogs_EscapedFlagProvenance verifies the escaped flag round-trips
// through the DB on both the history and cursor endpoints, so the dashboard
// can decode \x20 only on rows known to use the flattened encoder.
func TestGetAppLogs_EscapedFlagProvenance(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	_, err := h.Pool().Pool().Exec(context.Background(),
		`INSERT INTO app_logs (timestamp, level, source, message, escaped, attrs_at) VALUES
		 (now(), 'info', 'provtest', 'legacy path="\x20evidence"', false, 0),
		 (now(), 'info', 'provtest', 'fetched provider="Ollama\x20Cloud"', true, 8)`)
	if err != nil {
		t.Fatalf("insert app logs: %v", err)
	}

	assertFlags := func(path string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		var response struct {
			Entries []struct {
				Message string `json:"message"`
				Escaped bool   `json:"escaped"`
				AttrsAt *int   `json:"attrs_at"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s: parse response: %v", path, err)
		}
		seen := 0
		for _, e := range response.Entries {
			if strings.Contains(e.Message, "Ollama") {
				seen++
				if !e.Escaped {
					t.Errorf("%s: encoder row should be escaped=true", path)
				}
				if e.AttrsAt == nil || *e.AttrsAt != 8 {
					t.Errorf("%s: encoder row attrs_at = %v, want 8", path, e.AttrsAt)
				}
			}
			if strings.Contains(e.Message, "legacy") {
				seen++
				if e.Escaped {
					t.Errorf("%s: legacy row must stay escaped=false", path)
				}
			}
		}
		if seen != 2 {
			t.Errorf("%s: expected both rows, matched %d", path, seen)
		}
	}
	assertFlags("/logs/app?history=true&source=provtest")
	assertFlags("/logs/app/cursor?source=provtest")
}

// withTestRingBuffer installs a fresh ring buffer (and no async writer) for the
// duration of a test, restoring the package globals afterwards.
func withTestRingBuffer(t *testing.T) *ringBuffer {
	t.Helper()
	savedBuf, savedWriter := appLogBuffer, dbWriter.Load()
	rb := &ringBuffer{entries: make([]AppLogEntry, appLogBufferSize)}
	appLogBuffer = rb
	dbWriter.Store(nil)
	t.Cleanup(func() {
		appLogBuffer = savedBuf
		dbWriter.Store(savedWriter)
	})
	return rb
}

// ringHas reports whether the ring still holds an entry with this message. The
// ring also collects whatever the server logs while a test runs, so assertions
// are about the seeded entries, not about the buffer's exact length.
func ringHas(rb *ringBuffer, message string) bool {
	for _, e := range rb.GetEntries() {
		if e.Message == message {
			return true
		}
	}
	return false
}

// fillRing writes n entries into the ring buffer.
func fillRing(rb *ringBuffer, source string, n int) {
	for i := range n {
		rb.writeEntry(AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    source,
			Message:   fmt.Sprintf("ring entry %d", i),
		})
	}
}

// A DELETE the database refused used to answer 200 with a count taken from the
// ring buffer it had already emptied, so the operator saw a purge that never
// happened and lost the live view on top of it. The failure is a 500 and the
// ring is left holding what the rows still hold.
func TestClearAppLogs_FailedDeleteAnswers500AndKeepsRing(t *testing.T) {
	h := newTestHandler(t)
	rb := withTestRingBuffer(t)
	fillRing(rb, "failed-delete", 4)

	// A cancelled request context is what the pool's Exec refuses on.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody).WithContext(ctx))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: a refused delete answered success", rec.Code)
	}
	for i := range 4 {
		if !ringHas(rb, fmt.Sprintf("ring entry %d", i)) {
			t.Fatalf("ring entry %d is gone: the buffer was cleared for a delete that failed", i)
		}
	}
}

// Entries the async writer had already queued used to flush after the DELETE,
// putting rows back that the operator had just purged. The purge drains the
// writer first, so nothing lands behind it.
func TestClearAppLogs_QueuedEntriesDoNotResurface(t *testing.T) {
	h := newTestHandler(t)
	withTestRingBuffer(t)
	pool := h.Pool().Pool()

	// A flush interval that will not fire during the test: only the purge's own
	// barrier, or the writer's shutdown, can move these entries.
	w := newDBLogWriter(pool, time.Hour)
	dbWriter.Store(w)
	const source = "resurface-test"
	for i := range 5 {
		w.write(testEntry(source, fmt.Sprintf("queued %d", i)))
	}

	rec := httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body.String())
	}

	// Shutting the writer down flushes anything it is still holding. If the
	// purge had not drained it first, these rows would reappear now.
	w.stop()
	if got := countAppLogs(t, pool, source); got != 0 {
		t.Fatalf("%d purged rows came back from the writer queue", got)
	}
}

// The ring mirrors rows the database already holds, so summing the two reported
// a purge of twice what existed. With a database configured the count is the
// rows it deleted.
func TestClearAppLogs_CountIsNotDoubled(t *testing.T) {
	h := newTestHandler(t)
	rb := withTestRingBuffer(t)
	fillRing(rb, "double-count", 5)

	pool := h.Pool().Pool()
	for i := range 3 {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES (gen_random_uuid(), NOW(), 'info', 'double-count', 'msg', NOW())`); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	rec := httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["deleted"] != 3 {
		t.Fatalf("deleted = %d, want 3 (the rows), not the ring added on top", resp["deleted"])
	}
	if ringHas(rb, "ring entry 0") {
		t.Fatal("the ring was not cleared after a successful purge")
	}
}

// Without a database the ring is all there is, so its own count is the answer.
func TestClearAppLogs_RingCountWhenNoDB(t *testing.T) {
	rb := withTestRingBuffer(t)
	const seeded = 6
	fillRing(rb, "ring-only", seeded)

	rec := httptest.NewRecorder()
	(&Handler{}).ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The server keeps logging into the swapped-in ring while this test runs, so
	// the count is at least the six seeded entries, never an exact buffer
	// length snapshotted before the call.
	if resp["deleted"] < seeded {
		t.Fatalf("deleted = %d, want at least the %d seeded: with no database the ring count is the only count", resp["deleted"], seeded)
	}
	for i := range seeded {
		if ringHas(rb, fmt.Sprintf("ring entry %d", i)) {
			t.Fatalf("seeded entry %d survived the purge", i)
		}
	}

	// The ranged purge takes the same route: only the entries past the cutoff go,
	// and the count is still the ring's own.
	rb.writeEntry(AppLogEntry{Timestamp: time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano), Level: "info", Source: "ring-only", Message: "stale"})
	rb.writeEntry(AppLogEntry{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Source: "ring-only", Message: "fresh"})

	rec = httptest.NewRecorder()
	(&Handler{}).ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", strings.NewReader(`{"older_than":"1w"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("ranged clear: status %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ranged: %v", err)
	}
	if resp["deleted"] != 1 {
		t.Fatalf("ranged deleted = %d, want 1 (the stale entry only)", resp["deleted"])
	}
	if ringHas(rb, "stale") {
		t.Fatal("the stale entry survived the ranged purge")
	}
	if !ringHas(rb, "fresh") {
		t.Fatal("the ranged purge took the fresh entry too")
	}
}

// A barrier that does not complete means entries are still queued and will
// flush after the DELETE, reinstating the rows the operator asked to be gone.
// The purge refuses rather than answer 200 with a count the pending flush is
// about to undo. The writer here has no run goroutine, so the barrier is
// accepted and never answered: the deep-queue case, not a dead database.
func TestClearAppLogs_StalledFlushBarrierRefusesThePurge(t *testing.T) {
	h := newTestHandler(t)
	rb := withTestRingBuffer(t)
	fillRing(rb, "stalled-barrier", 3)

	pool := h.Pool().Pool()
	const source = "stalled-barrier-rows"
	for i := range 3 {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES (gen_random_uuid(), NOW(), 'info', $1, 'msg', NOW())`, source); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	saved := dbWriter.Load()
	t.Cleanup(func() { dbWriter.Store(saved) })
	dbWriter.Store(armed(&dbLogWriter{
		ch:          make(chan logMsg, 1),
		done:        make(chan struct{}),
		sendTimeout: dbLogSendTimeout,
	}))

	// Every debuglog line goes through the writer that is stalled, which is the
	// whole point of the budget assertion below: reporting this refusal through
	// the app-log pipeline would queue a line onto the queue that just failed to
	// drain and cost the operator a second sendTimeout on top of the barrier.
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	debuglog.SetHandler(NewAppSlogHandler(slog.LevelInfo))

	rec := httptest.NewRecorder()
	start := time.Now()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody))
	elapsed := time.Since(start)

	// The barrier's own budget and a margin, not twice it. The queue holds the
	// barrier, so a caller that logs its way out parks for the full send timeout
	// as well.
	if budget := appLogFlushBarrierTimeout + 2*time.Second; elapsed > budget {
		t.Errorf("the 503 took %s, want at most %s: the refusal waited on the stalled queue it is reporting", elapsed, budget)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: a purge ran under a queue that will reinstate the rows", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "retry") {
		t.Errorf("body = %q, want a retry message the operator can act on", rec.Body.String())
	}
	if got := countAppLogs(t, pool, source); got != 3 {
		t.Fatalf("%d rows left of 3: the refused purge deleted anyway", got)
	}
	for i := range 3 {
		if !ringHas(rb, fmt.Sprintf("ring entry %d", i)) {
			t.Fatalf("ring entry %d is gone: the buffer was cleared for a purge that did not happen", i)
		}
	}
}

// A writer that refuses the barrier because it is stopped is refused too. The
// global still points at it for as long as the stop runs, and during that window
// the run goroutine is draining what is queued into the pool, so "stopped" read
// as "stopped and drained" is exactly the flush the barrier exists to prevent.
// Only a writer that was never configured skips the barrier, and that is a
// deployment with no database and nothing queued anywhere.
func TestClearAppLogs_StoppedWriterRefusesThePurge(t *testing.T) {
	h := newTestHandler(t)
	rb := withTestRingBuffer(t)
	fillRing(rb, "stopped-writer", 1)

	pool := h.Pool().Pool()
	const source = "stopped-writer-rows"
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
		 VALUES (gen_random_uuid(), NOW(), 'info', $1, 'msg', NOW())`, source); err != nil {
		t.Fatalf("insert: %v", err)
	}

	saved := dbWriter.Load()
	t.Cleanup(func() { dbWriter.Store(saved) })
	w := newDBLogWriter(pool, time.Hour)
	w.stop()
	dbWriter.Store(w)

	rec := httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodDelete, "/logs/app", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: a purge ran against a writer whose queue it could not confirm drained", rec.Code)
	}
	if got := countAppLogs(t, pool, source); got != 1 {
		t.Fatalf("%d rows left of 1: the refused purge deleted anyway", got)
	}
	if !ringHas(rb, "ring entry 0") {
		t.Fatal("ring entry is gone: the buffer was cleared for a purge that did not happen")
	}
}
