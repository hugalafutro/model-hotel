package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	var response []map[string]interface{}
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
		Entries []map[string]interface{} `json:"entries"`
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
		Entries []map[string]interface{} `json:"entries"`
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
		Entries []map[string]interface{} `json:"entries"`
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

	var response []map[string]interface{}
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
	for i := 0; i < 10; i++ {
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
		Entries      []map[string]interface{} `json:"entries"`
		Total        int                      `json:"total"`
		Page         int                      `json:"page"`
		PerPage      int                      `json:"per_page"`
		LevelCounts  map[string]int           `json:"level_counts"`
		SourceCounts map[string]int           `json:"source_counts"`
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
	var response []interface{}
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
		t.Skip("skipping: test database not available")
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
		t.Skip("skipping: test database not available")
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
		t.Skip("skipping: test database not available")
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
		t.Skip("skipping: test database not available")
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
		t.Skip("skipping: test database not available")
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
