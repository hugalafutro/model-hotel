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

func TestRingBufferClearOlderThan(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	now := time.Now().UTC()
	// Five entries spaced one hour apart, oldest first (4h ago .. 0h ago).
	for i := 4; i >= 0; i-- {
		appLogBuffer.writeEntry(AppLogEntry{
			Timestamp: now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("message %d", i),
		})
	}

	// Cutoff 2h30m ago removes the 4h and 3h entries (2 of 5).
	removed := appLogBuffer.ClearOlderThan(now.Add(-150 * time.Minute))
	if removed != 2 {
		t.Fatalf("expected 2 entries removed, got %d", removed)
	}

	entries := appLogBuffer.GetEntries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 survivors, got %d", len(entries))
	}
	// Survivors must be the newest three, still oldest-first.
	for i, e := range entries {
		ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			t.Fatalf("unparseable survivor timestamp %q: %v", e.Timestamp, err)
		}
		if ts.Before(now.Add(-150 * time.Minute)) {
			t.Errorf("survivor %d is older than the cutoff: %s", i, e.Timestamp)
		}
	}
}

func TestRingBufferClearOlderThan_KeepsUnparseable(t *testing.T) {
	InitAppLogBuffer(nil)
	defer func() {
		appLogBuffer = nil
		dbWriter = nil
	}()

	appLogBuffer.writeEntry(AppLogEntry{Timestamp: "not-a-timestamp", Message: "keep me"})
	appLogBuffer.writeEntry(AppLogEntry{
		Timestamp: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
		Message:   "drop me",
	})

	removed := appLogBuffer.ClearOlderThan(time.Now().UTC())
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	entries := appLogBuffer.GetEntries()
	if len(entries) != 1 || entries[0].Message != "keep me" {
		t.Fatalf("expected only the undateable entry to survive, got %+v", entries)
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

func TestAppSlogHandler_HandleDebugLevel(t *testing.T) {
	// Save and restore app log buffer
	origBuffer := appLogBuffer
	defer func() { appLogBuffer = origBuffer }()

	// Initialize with proper capacity
	InitAppLogBuffer(nil)
	buf := appLogBuffer
	// Clear any existing entries
	buf.Clear()

	handler := NewAppSlogHandler(slog.LevelDebug)

	// Test Debug-level record maps to "debug"
	debugRecord := slog.NewRecord(time.Now(), slog.LevelDebug, "access: request", 0)
	debugRecord.AddAttrs(slog.String("method", "GET"), slog.String("path", "/api/logs/app/cursor"))

	if err := handler.Handle(context.Background(), debugRecord); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	entries := buf.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != "debug" {
		t.Errorf("expected level %q, got %q", "debug", entries[0].Level)
	}

	// Test Info-level record maps to "info" (not "debug")
	// This verifies the fix for the bug where r.Level >= slog.LevelDebug
	// matched Info records too since slog.LevelDebug=-4 and slog.LevelInfo=0
	infoRecord := slog.NewRecord(time.Now(), slog.LevelInfo, "server: started", 0)
	infoRecord.AddAttrs(slog.String("method", "POST"))

	if err := handler.Handle(context.Background(), infoRecord); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	entries = buf.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[1].Level != "info" {
		t.Errorf("expected level %q, got %q", "info", entries[1].Level)
	}
}

func TestAppSlogHandler_HandleStderrLevelPrefix(t *testing.T) {
	// Save and restore
	origBuffer := appLogBuffer
	defer func() { appLogBuffer = origBuffer }()

	// Create buffer directly without calling InitAppLogBuffer (which sets up global stderr filter)
	buf := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	appLogBuffer = buf

	var stderrBuf bytes.Buffer
	handler := &appSlogHandler{
		level:  slog.LevelDebug,
		stderr: &stderrLogFilter{dst: &stderrBuf},
	}

	// Test error record includes level=ERROR in stderr output (error always passes filter)
	record := slog.NewRecord(time.Now(), slog.LevelError, "access: request failed", 0)
	record.AddAttrs(slog.String("method", "GET"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(stderrBuf.String(), "level=ERROR") {
		t.Errorf("expected stderr output to contain 'level=ERROR', got %q", stderrBuf.String())
	}

	// Test warning record includes level=WARN
	stderrBuf.Reset()
	record = slog.NewRecord(time.Now(), slog.LevelWarn, "server: slow response", 0)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(stderrBuf.String(), "level=WARN") {
		t.Errorf("expected stderr output to contain 'level=WARN', got %q", stderrBuf.String())
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
	// Reset the global cache so this test isn't affected by other tests
	// that may have populated it via a real DB connection.
	appLogCountCache.Lock()
	appLogCountCache.levelCounts = nil
	appLogCountCache.sourceCounts = nil
	appLogCountCache.fetchedAt = time.Time{}
	appLogCountCache.Unlock()

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
	// Establish the nil-buffer precondition explicitly: appLogBuffer is a global
	// that an earlier test may have initialized, so we cannot rely on it already
	// being nil (the defer above only restores it afterwards).
	appLogBuffer = nil
	dbWriter = nil
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

// TestGetAppLogs_HistorySuccessWithDB tests the full success path of
// getAppLogsHistory: entries are read from the DB, scanned, and returned
// with proper pagination metadata including level/source counts.
func TestGetAppLogs_HistorySuccessWithDB(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	pool := h.Pool().Pool()
	ctx := context.Background()

	// Insert app log entries with varied levels and sources
	levels := []string{"info", "warning", "error"}
	sources := []string{"proxy", "auth"}
	for i := 0; i < 5; i++ {
		level := levels[i%len(levels)]
		source := sources[i%len(sources)]
		_, err := pool.Exec(ctx,
			`INSERT INTO app_logs (timestamp, level, source, message)
			 VALUES (NOW() + ($1 || ' seconds')::interval, $2, $3, $4)`,
			fmt.Sprintf("%d", i), level, source, fmt.Sprintf("history success test message %d", i))
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&per_page=3", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify pagination
	if resp.PerPage != 3 {
		t.Errorf("expected PerPage=3, got %d", resp.PerPage)
	}
	if resp.Page != 1 {
		t.Errorf("expected Page=1 (first page), got %d", resp.Page)
	}
	if resp.Total < 5 {
		t.Errorf("expected Total >= 5, got %d", resp.Total)
	}
	if len(resp.Entries) != 3 {
		t.Errorf("expected 3 entries on first page, got %d", len(resp.Entries))
	}

	// Verify entries have required fields
	for i, entry := range resp.Entries {
		if entry.Timestamp == "" {
			t.Errorf("entry %d: expected non-empty Timestamp", i)
		}
		if entry.Level == "" {
			t.Errorf("entry %d: expected non-empty Level", i)
		}
		if entry.Source == "" {
			t.Errorf("entry %d: expected non-empty Source", i)
		}
		if entry.Message == "" {
			t.Errorf("entry %d: expected non-empty Message", i)
		}
	}

	// Verify level counts are populated (should have at least one entry per level)
	if len(resp.LevelCounts) == 0 {
		t.Error("expected non-empty LevelCounts")
	}
}

// TestGetAppLogs_HistoryPagination tests that the history endpoint
// correctly handles page navigation.
func TestGetAppLogs_HistoryPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	pool := h.Pool().Pool()
	ctx := context.Background()

	// Insert more entries than one page can hold
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO app_logs (timestamp, level, source, message)
			 VALUES (NOW() + ($1 || ' seconds')::interval, $2, $3, $4)`,
			fmt.Sprintf("%d", i), "info", "pagination-test", fmt.Sprintf("page test %d", i))
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Request page 2 with per_page=2
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&page=2&per_page=2", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Page != 2 {
		t.Errorf("expected Page=2, got %d", resp.Page)
	}
	if resp.PerPage != 2 {
		t.Errorf("expected PerPage=2, got %d", resp.PerPage)
	}
	if len(resp.Entries) > 2 {
		t.Errorf("expected at most 2 entries on page 2, got %d", len(resp.Entries))
	}
}

// TestAppSlogHandlerJSONOutput verifies that in JSON mode the stderr line is a
// single valid JSON object carrying the reserved keys plus slog attrs as
// fields. A warning record is used so it passes the filter's level gate without
// depending on the global debug level.
func TestAppSlogHandlerJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	h := &appSlogHandler{
		level:      slog.LevelDebug,
		stderr:     &stderrLogFilter{dst: &buf},
		jsonOutput: true,
	}

	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "proxy: failover triggered", 0)
	rec.AddAttrs(slog.String("provider", "groq"), slog.Int("attempt", 2))
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected a JSON line on stderr, got nothing")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stderr line is not valid JSON: %v\nline: %s", err, out)
	}
	if got["level"] != "warning" {
		t.Errorf("level = %v, want warning", got["level"])
	}
	if got["source"] != "proxy" {
		t.Errorf("source = %v, want proxy", got["source"])
	}
	if got["msg"] != "failover triggered" {
		t.Errorf("msg = %v, want %q", got["msg"], "failover triggered")
	}
	if got["provider"] != "groq" {
		t.Errorf("provider field = %v, want groq", got["provider"])
	}
	if got["attempt"] != "2" {
		t.Errorf("attempt field = %v, want \"2\"", got["attempt"])
	}
}

// TestParseLogLineJSON verifies the stderr filter's classifier understands
// JSON log lines, so the level gate and source suppression work identically in
// JSON mode. This is what keeps an info JSON line from being misclassified (and
// dropped) by the text-only heuristics.
func TestParseLogLineJSON(t *testing.T) {
	line := `{"attempt":"2","level":"info","msg":"routing to provider","provider":"groq","source":"proxy","time":"2026-06-13T00:00:00Z"}`
	source, level, msg := parseLogLine(line)
	if source != "proxy" {
		t.Errorf("source = %q, want proxy", source)
	}
	if level != "info" {
		t.Errorf("level = %q, want info", level)
	}
	if msg != "routing to provider" {
		t.Errorf("msg = %q, want %q", msg, "routing to provider")
	}

	// A non-JSON line must still fall through to the text parser.
	source, level, _ = parseLogLine("2026/06/13 00:00:00 proxy: something happened")
	if source != "proxy" {
		t.Errorf("text fallthrough source = %q, want proxy", source)
	}
	if level == "" {
		t.Error("text fallthrough should still detect a level")
	}
}
