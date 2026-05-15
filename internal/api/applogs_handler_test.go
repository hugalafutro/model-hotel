package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ringBuffer tests
// ---------------------------------------------------------------------------

func TestRingBufferWriteEntry(t *testing.T) {
	InitAppLogBuffer(nil) // Initialize with nil pool for pure tests
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	entry := AppLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     "info",
		Source:    "test",
		Message:   "test message",
	}

	appLogBuffer.writeEntry(entry)
	entries := appLogBuffer.GetEntries()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Message != "test message" {
		t.Errorf("expected message %q, got %q", "test message", entries[0].Message)
	}
}

func TestRingBufferWriteEntry_WrapAround(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	// Fill the buffer to capacity
	for i := 0; i < appLogBufferSize; i++ {
		entry := AppLogEntry{
			Timestamp: time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		}
		appLogBuffer.writeEntry(entry)
	}

	// Add one more to trigger wrap-around
	newEntry := AppLogEntry{
		Timestamp: time.Now().UTC().Add(time.Duration(appLogBufferSize) * time.Second).Format(time.RFC3339Nano),
		Level:     "info",
		Source:    "test",
		Message:   "wrapped message",
	}
	appLogBuffer.writeEntry(newEntry)

	entries := appLogBuffer.GetEntries()
	// Should still have appLogBufferSize entries
	if len(entries) != appLogBufferSize {
		t.Errorf("expected %d entries after wrap-around, got %d", appLogBufferSize, len(entries))
	}
	// When buffer is full, the oldest entry (at index 0) should be the one that was overwritten
	// which is "message 1" (since we wrote 500 entries, then one more that overwrites the first)
	if entries[0].Message != "message 1" {
		t.Errorf("expected first entry to be message 1 (oldest after wrap), got %q", entries[0].Message)
	}
	// The newest entry should be the wrapped message (last in the array)
	if entries[len(entries)-1].Message != "wrapped message" {
		t.Errorf("expected last entry to be wrapped message (newest), got %q", entries[len(entries)-1].Message)
	}
}

func TestRingBufferGetEntries_Empty(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	entries := appLogBuffer.GetEntries()
	if entries != nil {
		t.Errorf("expected nil for empty buffer, got %v", entries)
	}
}

func TestRingBufferGetEntries_Order(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	// Add entries in order
	for i := 0; i < 5; i++ {
		entry := AppLogEntry{
			Timestamp: time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		}
		appLogBuffer.writeEntry(entry)
	}

	entries := appLogBuffer.GetEntries()
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	// Should be in chronological order (oldest first)
	for i := 0; i < 5; i++ {
		if !strings.Contains(entries[i].Message, fmt.Sprintf("message %d", i)) {
			t.Errorf("entry %d should contain 'message %d', got %q", i, i, entries[i].Message)
		}
	}
}

func TestRingBufferClear(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	// Add some entries
	for i := 0; i < 10; i++ {
		entry := AppLogEntry{
			Timestamp: time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		}
		appLogBuffer.writeEntry(entry)
	}

	cleared := appLogBuffer.Clear()
	if cleared != 10 {
		t.Errorf("expected 10 entries cleared, got %d", cleared)
	}

	entries := appLogBuffer.GetEntries()
	if entries != nil {
		t.Errorf("expected nil after clear, got %v", entries)
	}
}

// ---------------------------------------------------------------------------
// dbLogWriter tests
// ---------------------------------------------------------------------------

func TestNewDBLogWriter(t *testing.T) {
	// Test constructor - just verify it doesn't panic and returns non-nil
	writer := newDBLogWriter(nil)
	if writer == nil {
		t.Error("newDBLogWriter should return non-nil writer")
	}
	// Note: the goroutine is running, but we can't easily test it without mocking time
}

func TestDBLogWriterWrite_Timeout(t *testing.T) {
	// Create a writer with a nil pool (no DB)
	writer := newDBLogWriter(nil)
	defer writer.stop()

	entry := AppLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     "info",
		Source:    "test",
		Message:   "test message",
	}

	// This should not block indefinitely due to timeout
	done := make(chan struct{})
	go func() {
		writer.write(entry)
		close(done)
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(2 * dbLogSendTimeout):
		t.Error("write should not block longer than timeout")
	}
}

// ---------------------------------------------------------------------------
// AppSlogHandler tests
// ---------------------------------------------------------------------------

func TestNewAppSlogHandler(t *testing.T) {
	handler := NewAppSlogHandler(slog.LevelInfo)
	if handler == nil {
		t.Error("NewAppSlogHandler should return non-nil handler")
	}

	appHandler, ok := handler.(*appSlogHandler)
	if !ok {
		t.Errorf("expected *appSlogHandler, got %T", handler)
	}
	if appHandler.level != slog.LevelInfo {
		t.Errorf("expected level Info, got %v", appHandler.level)
	}
}

func TestAppSlogHandlerEnabled(t *testing.T) {
	handler := NewAppSlogHandler(slog.LevelWarn)

	testCases := []struct {
		level    slog.Level
		expected bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, false},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}

	for _, tc := range testCases {
		t.Run(tc.level.String(), func(t *testing.T) {
			result := handler.Enabled(context.Background(), tc.level)
			if result != tc.expected {
				t.Errorf("Enabled(%v) = %v, want %v", tc.level, result, tc.expected)
			}
		})
	}
}

func TestAppSlogHandlerWithAttrs(t *testing.T) {
	handler := NewAppSlogHandler(slog.LevelInfo)

	attrs := []slog.Attr{
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
	}

	newHandler := handler.WithAttrs(attrs)
	if newHandler == nil {
		t.Error("WithAttrs should return non-nil handler")
	}

	appHandler, ok := newHandler.(*appSlogHandler)
	if !ok {
		t.Errorf("expected *appSlogHandler, got %T", newHandler)
	}

	if len(appHandler.attrs) != 2 {
		t.Errorf("expected 2 attrs, got %d", len(appHandler.attrs))
	}
}

func TestAppSlogHandlerWithGroup(t *testing.T) {
	handler := NewAppSlogHandler(slog.LevelInfo)

	newHandler := handler.WithGroup("test")
	if newHandler == nil {
		t.Error("WithGroup should return non-nil handler")
	}

	appHandler, ok := newHandler.(*appSlogHandler)
	if !ok {
		t.Errorf("expected *appSlogHandler, got %T", newHandler)
	}

	if appHandler.group != "test" {
		t.Errorf("expected group 'test', got %q", appHandler.group)
	}

	// Test nested groups
	nestedHandler := newHandler.WithGroup("nested")
	appNestedHandler, _ := nestedHandler.(*appSlogHandler)
	if appNestedHandler.group != "test.nested" {
		t.Errorf("expected nested group 'test.nested', got %q", appNestedHandler.group)
	}
}

// ---------------------------------------------------------------------------
// stderrLogFilter tests
// ---------------------------------------------------------------------------

func TestStderrLogFilterWrite(t *testing.T) {
	var buf bytes.Buffer
	filter := &stderrLogFilter{dst: &buf}

	// Test error level - should be written
	_, err := filter.Write([]byte("2026/04/28 09:55:43 [proxy] ERROR connection failed\n"))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("expected error to be written to stderr, got %q", buf.String())
	}

	// Test warning level - should be written
	buf.Reset()
	_, err = filter.Write([]byte("2026/04/28 09:55:43 [auth] WARN rate limit approaching\n"))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if !strings.Contains(buf.String(), "WARN") {
		t.Errorf("expected warning to be written to stderr, got %q", buf.String())
	}

	// Test info level - should NOT be written
	buf.Reset()
	_, err = filter.Write([]byte("2026/04/28 09:55:43 [proxy] INFO request received\n"))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if strings.Contains(buf.String(), "INFO") {
		t.Errorf("expected info to NOT be written to stderr, got %q", buf.String())
	}
}

func TestStderrLogFilterWrite_SuppressedSource(t *testing.T) {
	var buf bytes.Buffer
	filter := &stderrLogFilter{dst: &buf}

	// Add a source to suppress
	stderrSuppressSources["noisy"] = true
	defer func() {
		delete(stderrSuppressSources, "noisy")
	}()

	// Even errors from suppressed sources should not be written
	_, err := filter.Write([]byte("2026/04/28 09:55:43 [noisy] ERROR some error\n"))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if strings.Contains(buf.String(), "ERROR") {
		t.Errorf("expected suppressed source to NOT be written, got %q", buf.String())
	}
}

func TestStderrLogFilterWrite_MultiLine(t *testing.T) {
	var buf bytes.Buffer
	filter := &stderrLogFilter{dst: &buf}

	input := "2026/04/28 09:55:43 [proxy] ERROR first line\n2026/04/28 09:55:44 [auth] INFO second line\n2026/04/28 09:55:45 [proxy] ERROR third line\n"
	_, err := filter.Write([]byte(input))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}

	// Should only contain error lines
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 error lines, got %d: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "ERROR") {
			t.Errorf("expected only error lines, got %q", line)
		}
	}
}

// ---------------------------------------------------------------------------
// InitAppLogBuffer tests
// ---------------------------------------------------------------------------

func TestInitAppLogBuffer(t *testing.T) {
	// Save original values
	origBuffer := appLogBuffer
	origWriter := dbWriter
	origOutput := log.Writer()
	defer func() {
		appLogBuffer = origBuffer
		dbWriter = origWriter
		log.SetOutput(origOutput)
	}()

	// Test with nil pool
	InitAppLogBuffer(nil)
	if appLogBuffer == nil {
		t.Error("InitAppLogBuffer should initialize appLogBuffer")
	}
	if dbWriter != nil {
		t.Error("InitAppLogBuffer with nil pool should not initialize dbWriter")
	}

	// Verify log output is set
	currentOutput := log.Writer()
	if currentOutput == nil {
		t.Error("log output should be set")
	}
}

// ---------------------------------------------------------------------------
// StopAppLogWriter tests
// ---------------------------------------------------------------------------

func TestStopAppLogWriter(t *testing.T) {
	// Save original values
	origWriter := dbWriter
	defer func() {
		dbWriter = origWriter
	}()

	// Create a writer
	InitAppLogBuffer(nil)
	writer := newDBLogWriter(nil)
	dbWriter = writer

	// Stop it
	StopAppLogWriter()

	if dbWriter != nil {
		t.Error("StopAppLogWriter should set dbWriter to nil")
	}
}

// ---------------------------------------------------------------------------
// Write (io.Writer) tests
// ---------------------------------------------------------------------------

func TestRingBufferWrite_MultiLine(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	input := "2026/04/28 09:55:43 [proxy] INFO first\n2026/04/28 09:55:44 [auth] ERROR second\n"
	n, err := appLogBuffer.Write([]byte(input))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(input) {
		t.Errorf("expected %d bytes written, got %d", len(input), n)
	}

	entries := appLogBuffer.GetEntries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestRingBufferWrite_EmptyLines(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	input := "\n\n2026/04/28 09:55:43 [proxy] INFO message\n\n"
	n, err := appLogBuffer.Write([]byte(input))
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(input) {
		t.Errorf("expected %d bytes written, got %d", len(input), n)
	}

	entries := appLogBuffer.GetEntries()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (empty lines skipped), got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// getAppLogCounts tests
// ---------------------------------------------------------------------------

func TestGetAppLogCounts_Cache(t *testing.T) {
	h := &Handler{dbPool: nil}

	// First call should return default counts
	levelCounts, sourceCounts := h.getAppLogCounts(context.Background())
	if len(levelCounts) != 3 {
		t.Errorf("expected 3 level counts, got %d", len(levelCounts))
	}
	if levelCounts["info"] != 0 || levelCounts["warning"] != 0 || levelCounts["error"] != 0 {
		t.Error("expected all level counts to be 0")
	}
	if len(sourceCounts) != 0 {
		t.Errorf("expected empty source counts, got %d", len(sourceCounts))
	}

	// With nil dbPool, cache is NOT populated (returns early)
	// This is the current behavior - cache only populates when there's a real DB connection
	appLogCountCache.RLock()
	cacheEmpty := appLogCountCache.levelCounts == nil
	appLogCountCache.RUnlock()
	if !cacheEmpty {
		t.Error("cache should NOT be populated when dbPool is nil")
	}
}

// ---------------------------------------------------------------------------
// GetAppLogs handler tests
// ---------------------------------------------------------------------------

func TestGetAppLogs_NilBuffer(t *testing.T) {
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()
	// appLogBuffer is nil by default if InitAppLogBuffer hasn't been called
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/app-logs", http.NoBody)
	rr := httptest.NewRecorder()

	h.GetAppLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := strings.TrimSpace(rr.Body.String())
	if body != "[]" {
		t.Errorf("expected body [], got %q", body)
	}
}

func TestGetAppLogs_HistoryNilDBPool(t *testing.T) {
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/app-logs?history=true", http.NoBody)
	rr := httptest.NewRecorder()

	h.GetAppLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected Total 0, got %d", resp.Total)
	}
	if resp.Page != 0 {
		t.Errorf("expected Page 0, got %d", resp.Page)
	}
	if resp.PerPage != 0 {
		t.Errorf("expected PerPage 0, got %d", resp.PerPage)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty Entries, got %d", len(resp.Entries))
	}
}

func TestGetAppLogs_WithLimitAndAfter(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	// Add 15 entries with different timestamps
	baseTime := time.Now().UTC()
	for i := 0; i < 15; i++ {
		entry := AppLogEntry{
			Timestamp: baseTime.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		}
		appLogBuffer.writeEntry(entry)
	}

	h := &Handler{dbPool: nil}

	// Test limit=5 - should return only last 5 entries
	req := httptest.NewRequest(http.MethodGet, "/api/app-logs?limit=5", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetAppLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	var entries []AppLogEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries with limit=5, got %d", len(entries))
	}
	// Should be the last 5 entries (messages 10-14)
	if len(entries) > 0 && entries[0].Message != "message 10" {
		t.Errorf("expected first entry to be message 10, got %q", entries[0].Message)
	}

	// Test after=<timestamp_of_entry_10> - should return entries after that timestamp
	afterTime := baseTime.Add(9 * time.Second).Format(time.RFC3339Nano) // timestamp of entry 9
	req = httptest.NewRequest(http.MethodGet, "/api/app-logs?after="+afterTime, http.NoBody)
	rr = httptest.NewRecorder()
	h.GetAppLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	entries = nil
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Should return entries 10-14 (5 entries after timestamp of entry 9)
	if len(entries) != 5 {
		t.Errorf("expected 5 entries after timestamp, got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].Message != "message 10" {
		t.Errorf("expected first entry after filter to be message 10, got %q", entries[0].Message)
	}
}

func TestClearAppLogs_WithBuffer(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	// Add some entries
	for i := 0; i < 5; i++ {
		entry := AppLogEntry{
			Timestamp: time.Now().UTC().Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		}
		appLogBuffer.writeEntry(entry)
	}

	// Verify entries exist
	entries := appLogBuffer.GetEntries()
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries before clear, got %d", len(entries))
	}

	// Create handler with nil dbPool (so DB delete is skipped)
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodDelete, "/api/app-logs", http.NoBody)
	rr := httptest.NewRecorder()

	h.ClearAppLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Verify buffer is cleared
	entries = appLogBuffer.GetEntries()
	if entries != nil {
		t.Errorf("expected nil entries after clear, got %v", entries)
	}
}

// TestGetAppLogs_HistoryWithDB tests that GetAppLogs with history=true
// returns log entries from the database.
func TestGetAppLogs_HistoryWithDB(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Insert app log entries directly via DB
	pool := h.Pool().Pool()
	ctx := context.Background()

	// Insert a few app log entries
	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO app_logs (timestamp, level, source, message)
			 VALUES (NOW(), $1, $2, $3)`,
			"info", "test", fmt.Sprintf("test message %d", i))
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Request with history=true - route is /logs/app
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total < 3 {
		t.Errorf("expected at least 3 entries, got %d", resp.Total)
	}

	// Verify entries contain our test messages
	found := 0
	for _, entry := range resp.Entries {
		if entry.Source == "test" && strings.Contains(entry.Message, "test message") {
			found++
		}
	}
	if found < 3 {
		t.Errorf("expected to find at least 3 test entries, got %d", found)
	}
}
