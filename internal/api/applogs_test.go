package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// stripLevelPrefix
// ---------------------------------------------------------------------------

func TestDetectLevel_Error(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"error word", "[proxy] error: connection refused"},
		{"ERROR uppercase", "ERROR failed"},
		{"fatal word", "[proxy] fatal: out of memory"},
		{"panic word", "panic: runtime error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectLevel(tc.line)
			if result != "error" {
				t.Errorf("detectLevel(%q) = %q, want %q", tc.line, result, "error")
			}
		})
	}
}

func TestDetectLevel_Warning(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"warn word", "[proxy] warn: slow response"},
		{"WARN uppercase", "WARN something"},
		{"warning word", "warning: deprecated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectLevel(tc.line)
			if result != "warning" {
				t.Errorf("detectLevel(%q) = %q, want %q", tc.line, result, "warning")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// wordMatch
// ---------------------------------------------------------------------------

func TestDetectLevel_Info(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"normal log", "[proxy] request processed"},
		{"INFO prefix", "INFO  something happened"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectLevel(tc.line)
			if result != "info" {
				t.Errorf("detectLevel(%q) = %q, want %q", tc.line, result, "info")
			}
		})
	}
}

func TestDetectLevel_Debug(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"debug word", "[proxy] debug: tracing"},
		{"DEBUG uppercase", "DEBUG something"},
		{"level=DEBUG prefix", "level=DEBUG trace output"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectLevel(tc.line)
			if result != "debug" {
				t.Errorf("detectLevel(%q) = %q, want %q", tc.line, result, "debug")
			}
		})
	}
}

func TestDetectLevel_NoFalsePositiveFromFieldNames(t *testing.T) {
	// Regression test: structured slog attrs like "error_chunks=0" or
	// "has_error=false" must NOT cause the line to be classified as error.
	tests := []struct {
		name string
		line string
		want string
	}{
		{"error_chunks field", "proxy: streaming finished error_chunks=0 has_error=false", "info"},
		{"has_error field", "proxy: completed has_error=false", "info"},
		{"error as word still matches", "proxy: error: connection refused", "error"},
		{"error in error_message field", "proxy: failed error_message=timeout", "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectLevel(tc.line)
			if got != tc.want {
				t.Errorf("detectLevel(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseLogLine
// ---------------------------------------------------------------------------

func TestParseLogLine_FullLine(t *testing.T) {
	line := "2024/01/15 09:30:00 [proxy] INFO  request received"
	source, level, msg := parseLogLine(line)
	if source != "proxy" {
		t.Errorf("expected source %q, got %q", "proxy", source)
	}
	if level != "info" {
		t.Errorf("expected level %q, got %q", "info", level)
	}
	if msg != "request received" {
		t.Errorf("expected message %q, got %q", "request received", msg)
	}
}

func TestParseLogLine_NoTimestamp(t *testing.T) {
	line := "[auth] ERROR invalid token"
	source, level, msg := parseLogLine(line)
	if source != "auth" {
		t.Errorf("expected source %q, got %q", "auth", source)
	}
	if level != "error" {
		t.Errorf("expected level %q, got %q", "error", level)
	}
	if msg != "invalid token" {
		t.Errorf("expected message %q, got %q", "invalid token", msg)
	}
}

func TestParseLogLine_PlainMessage(t *testing.T) {
	line := "something happened"
	source, level, msg := parseLogLine(line)
	if source != "" {
		t.Errorf("expected empty source, got %q", source)
	}
	if level != "info" {
		t.Errorf("expected info level for plain message, got %q", level)
	}
	if msg != "something happened" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		name string
		c    byte
		want bool
	}{
		{"lowercase_a", 'a', true},
		{"lowercase_z", 'z', true},
		{"uppercase_A", 'A', true},
		{"uppercase_Z", 'Z', true},
		{"digit_0", '0', true},
		{"digit_9", '9', true},
		{"underscore", '_', true},
		{"space", ' ', false},
		{"hyphen", '-', false},
		{"dot", '.', false},
		{"at_symbol", '@', false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWordChar(tt.c); got != tt.want {
				t.Errorf("isWordChar(%q) = %v, want %v", tt.c, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getAppLogsHistory and getAppLogCounts tests
// ---------------------------------------------------------------------------

func TestGetAppLogCounts_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	// Invalidate cache so the DB query path is exercised
	appLogCountCache.Lock()
	appLogCountCache.levelCounts = nil
	appLogCountCache.sourceCounts = nil
	appLogCountCache.fetchedAt = time.Time{}
	appLogCountCache.Unlock()

	h, _ := newTestHandlerWithRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	levelCounts, sourceCounts := h.getAppLogCounts(ctx)

	// With cancelled context, queries fail and return empty/zeroed maps
	if levelCounts == nil {
		t.Error("expected non-nil levelCounts map")
	}
	if sourceCounts == nil {
		t.Error("expected non-nil sourceCounts map")
	}
}

// ---------------------------------------------------------------------------
// dbLogWriter tests
// ---------------------------------------------------------------------------

func TestDBLogWriter_BatchSizeFlush(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Clean up before and after test
	pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'test'")
	defer pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'test'")

	w := newDBLogWriter(pool)
	defer w.stop()

	// Send 50 entries to trigger the batch-size flush path (lines 127-130)
	for i := range 50 {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("batch entry %d", i),
		}
	}

	// Poll until the batch-size flush lands in the DB or we time out. A fixed
	// sleep is flaky: the writer goroutine's schedule plus the 50-row INSERT can
	// exceed a short sleep under CI load, so a single check races the flush (this
	// mirrors TestDBLogWriter_TickerFlush).
	deadline := time.Now().Add(5 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs WHERE source = 'test'").Scan(&count)
		if err != nil {
			t.Fatalf("failed to query app_logs: %v", err)
		}
		if count >= 50 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count < 50 {
		t.Errorf("expected at least 50 entries in DB, got %d", count)
	}
}

func TestDBLogWriter_TickerFlush(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Clean up before test
	pool.Exec(context.Background(), "DELETE FROM app_logs WHERE source = 'ticker-test'")

	w := newDBLogWriter(pool)
	defer w.stop()

	// Send a few entries (less than 50) and wait for the ticker to flush
	for i := range 5 {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "ticker-test",
			Message:   fmt.Sprintf("ticker entry %d", i),
		}
	}

	// Poll until the ticker flushes the entries or we time out.
	// A fixed sleep is flaky because the 500ms ticker may not align with
	// the goroutine's select loop under CI load.
	deadline := time.Now().Add(5 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs WHERE source = 'ticker-test'").Scan(&count)
		if err != nil {
			t.Fatalf("failed to query app_logs: %v", err)
		}
		if count >= 5 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count < 5 {
		t.Errorf("expected at least 5 entries in DB after ticker flush, got %d", count)
	}
}

func TestDBLogWriter_FlushDBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}
	// Reduce flush interval for faster test
	orig := dbLogFlushInterval
	dbLogFlushInterval = 10 * time.Millisecond
	defer func() { dbLogFlushInterval = orig }()

	// Create a writer with a closed pool to trigger the Exec error path (lines 160-164)
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close() // Close immediately to cause DB errors

	w := newDBLogWriter(pool)
	defer w.stop()

	// Send entries — they'll be flushed but the DB write will fail silently
	for i := range 5 {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "flush-error-test",
			Message:   fmt.Sprintf("entry %d", i),
		}
	}

	// Wait for ticker flush (the batch is small, so ticker will flush it)
	time.Sleep(50 * time.Millisecond)

	// No panic or hang means the error was handled gracefully
}

func TestRingBuffer_WriteWithDBWriter(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	// Reduce flush interval for faster test
	orig := dbLogFlushInterval
	dbLogFlushInterval = 10 * time.Millisecond
	defer func() { dbLogFlushInterval = orig }()

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Save and restore global dbWriter
	origDBWriter := dbWriter
	dbWriter = newDBLogWriter(pool)
	defer func() {
		dbWriter.stop()
		dbWriter = origDBWriter
	}()

	rb := &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}

	// Write via ringBuffer.Write which calls dbWriter.write (lines 241-243)
	// Use slog-compatible format so parseLogLine extracts source correctly
	rb.Write([]byte("2026/01/01 00:00:00 INFO  ringbuf-db-test hello from ring buffer\n"))

	// Wait for flush
	time.Sleep(50 * time.Millisecond)

	// Verify the entry was written — check ring buffer has the entry
	entries := rb.GetEntries()
	found := false
	for _, e := range entries {
		if strings.Contains(e.Message, "hello from ring buffer") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected entry in ring buffer after Write")
	}
}

func TestStderrLogFilter_WriteError(t *testing.T) {
	// Test the dst.Write error path (lines 47-49)
	var errWriter errWriterMock
	f := &stderrLogFilter{dst: &errWriter}

	_, err := f.Write([]byte("level=error source=test message=oops\n"))
	if err == nil {
		t.Error("expected error from stderrLogFilter when dst.Write fails")
	}
}

func TestAppendAppLogFilters_NoFilters(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "", "", "")
	if len(conds) != 0 {
		t.Errorf("expected 0 conditions, got %d", len(conds))
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendAppLogFilters_LevelFilter(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "error", "", "", "", "")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0] != "level = $1" {
		t.Errorf("expected 'level = $1', got %q", conds[0])
	}
	if len(args) != 1 || args[0] != "error" {
		t.Errorf("expected args=['error'], got %v", args)
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendAppLogFilters_SourceFilter(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "proxy", "", "", "")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0] != "source = $1" {
		t.Errorf("expected 'source = $1', got %q", conds[0])
	}
	if args[0] != "proxy" {
		t.Errorf("expected args=['proxy'], got %v", args)
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendAppLogFilters_SearchFilter(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "timeout", "", "")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0] != "message ILIKE $1" {
		t.Errorf("expected 'message ILIKE $1', got %q", conds[0])
	}
	if args[0] != "%timeout%" {
		t.Errorf("expected args=['%%timeout%%'], got %v", args)
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendAppLogFilters_FromDate(t *testing.T) {
	from := "2024-06-01T00:00:00Z"
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "", from, "")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0] != "created_at >= $1" {
		t.Errorf("expected 'created_at >= $1', got %q", conds[0])
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	parsedFrom, _ := time.Parse(time.RFC3339, from)
	if args[0].(time.Time).UTC() != parsedFrom.UTC() {
		t.Errorf("expected %v, got %v", parsedFrom.UTC(), args[0])
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendAppLogFilters_ToDate(t *testing.T) {
	to := "2024-12-31T23:59:59Z"
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "", "", to)
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0] != "created_at <= $1" {
		t.Errorf("expected 'created_at <= $1', got %q", conds[0])
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	parsedTo, _ := time.Parse(time.RFC3339, to)
	if args[0].(time.Time).UTC() != parsedTo.UTC() {
		t.Errorf("expected %v, got %v", parsedTo.UTC(), args[0])
	}
	if idx != 2 {
		t.Errorf("expected argIdx=2, got %d", idx)
	}
}

func TestAppendAppLogFilters_InvalidFromDate(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "", "not-a-date", "")
	if len(conds) != 0 {
		t.Errorf("invalid from date should produce no condition, got %d", len(conds))
	}
	if len(args) != 0 {
		t.Errorf("invalid from date should produce no args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendAppLogFilters_InvalidToDate(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 1, "", "", "", "", "garbage")
	if len(conds) != 0 {
		t.Errorf("invalid to date should produce no condition, got %d", len(conds))
	}
	if len(args) != 0 {
		t.Errorf("invalid to date should produce no args, got %d", len(args))
	}
	if idx != 1 {
		t.Errorf("expected argIdx=1, got %d", idx)
	}
}

func TestAppendAppLogFilters_AllFilters(t *testing.T) {
	conds, args, idx := appendAppLogFilters(nil, nil, 3, "error", "proxy", "fail", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z")
	if len(conds) != 5 {
		t.Fatalf("expected 5 conditions, got %d", len(conds))
	}
	if conds[0] != "level = $3" {
		t.Errorf("first condition: expected 'level = $3', got %q", conds[0])
	}
	if conds[1] != "source = $4" {
		t.Errorf("second condition: expected 'source = $4', got %q", conds[1])
	}
	if conds[2] != "message ILIKE $5" {
		t.Errorf("third condition: expected 'message ILIKE $5', got %q", conds[2])
	}
	if conds[3] != "created_at >= $6" {
		t.Errorf("fourth condition: expected 'created_at >= $6', got %q", conds[3])
	}
	if conds[4] != "created_at <= $7" {
		t.Errorf("fifth condition: expected 'created_at <= $7', got %q", conds[4])
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d", len(args))
	}
	if idx != 8 {
		t.Errorf("expected argIdx=8, got %d", idx)
	}
}

func TestAppendAppLogFilters_PreservesExistingConditions(t *testing.T) {
	existingConds := []string{"some_col = $0"}
	existingArgs := []any{"existing"}
	conds, args, idx := appendAppLogFilters(existingConds, existingArgs, 2, "info", "", "", "", "")
	if len(conds) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conds))
	}
	if conds[0] != "some_col = $0" {
		t.Errorf("existing condition should be preserved, got %q", conds[0])
	}
	if conds[1] != "level = $2" {
		t.Errorf("new condition: expected 'level = $2', got %q", conds[1])
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "existing" {
		t.Errorf("existing arg should be preserved, got %v", args[0])
	}
	if args[1] != "info" {
		t.Errorf("new arg: expected 'info', got %v", args[1])
	}
	if idx != 3 {
		t.Errorf("expected argIdx=3, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// appendAppLogKeysetPredicate unit tests
// ---------------------------------------------------------------------------

func TestAppendAppLogKeysetPredicate_AfterDesc_ReturnsLessThan(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	conds, args, idx := appendAppLogKeysetPredicate(nil, nil, 1, cursor, "after", "DESC")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !strings.Contains(conds[0], "< $1") {
		t.Errorf("after+DESC should use '<', got %q", conds[0])
	}
	if !strings.Contains(conds[0], "< $3") {
		t.Errorf("after+DESC id comparison should use '<', got %q", conds[0])
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if idx != 4 {
		t.Errorf("expected argIdx=4, got %d", idx)
	}
}

func TestAppendAppLogKeysetPredicate_BeforeAsc_ReturnsLessThan(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	conds, _, _ := appendAppLogKeysetPredicate(nil, nil, 1, cursor, "before", "ASC")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !strings.Contains(conds[0], "< $1") {
		t.Errorf("before+ASC should use '<', got %q", conds[0])
	}
}

func TestAppendAppLogKeysetPredicate_AfterAsc_ReturnsGreaterThan(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	conds, _, _ := appendAppLogKeysetPredicate(nil, nil, 1, cursor, "after", "ASC")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !strings.Contains(conds[0], "> $1") {
		t.Errorf("after+ASC should use '>', got %q", conds[0])
	}
}

func TestAppendAppLogKeysetPredicate_BeforeDesc_ReturnsGreaterThan(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	conds, _, _ := appendAppLogKeysetPredicate(nil, nil, 1, cursor, "before", "DESC")
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !strings.Contains(conds[0], "> $1") {
		t.Errorf("before+DESC should use '>', got %q", conds[0])
	}
}

func TestAppendAppLogKeysetPredicate_ArgIndexOffset(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	conds, args, idx := appendAppLogKeysetPredicate(nil, nil, 5, cursor, "after", "DESC")
	if !strings.Contains(conds[0], "< $5") {
		t.Errorf("expected arg starting at $5, got %q", conds[0])
	}
	if !strings.Contains(conds[0], "< $7") {
		t.Errorf("expected id arg at $7, got %q", conds[0])
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if idx != 8 {
		t.Errorf("expected argIdx=8, got %d", idx)
	}
}

func TestAppendAppLogKeysetPredicate_PreservesExisting(t *testing.T) {
	ts := time.Now()
	cursor := appLogCursor{CreatedAt: ts, ID: "test-id"}
	existingConds := []string{"level = $1"}
	existingArgs := []any{"error"}
	conds, args, idx := appendAppLogKeysetPredicate(existingConds, existingArgs, 2, cursor, "after", "DESC")
	if len(conds) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conds))
	}
	if conds[0] != "level = $1" {
		t.Errorf("existing condition should be preserved, got %q", conds[0])
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args (1 existing + 3 new), got %d", len(args))
	}
	if idx != 5 {
		t.Errorf("expected argIdx=5, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// appLogWhereClause unit tests
// ---------------------------------------------------------------------------

func TestAppLogWhereClause_Empty(t *testing.T) {
	result := appLogWhereClause(nil)
	if result != "" {
		t.Errorf("expected empty string for nil conditions, got %q", result)
	}
	result = appLogWhereClause([]string{})
	if result != "" {
		t.Errorf("expected empty string for empty conditions, got %q", result)
	}
}

func TestAppLogWhereClause_SingleCondition(t *testing.T) {
	result := appLogWhereClause([]string{"level = $1"})
	if result != " WHERE level = $1" {
		t.Errorf("expected ' WHERE level = $1', got %q", result)
	}
}

func TestAppLogWhereClause_MultipleConditions(t *testing.T) {
	result := appLogWhereClause([]string{"level = $1", "source = $2"})
	if result != " WHERE level = $1 AND source = $2" {
		t.Errorf("expected conditions joined with AND, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// buildAppLogCursorQuery unit tests
// ---------------------------------------------------------------------------

// TestScanAppLogRow_ScanError tests that scanAppLogRow returns an error
// when the underlying row scan fails (e.g. wrong column count or type mismatch).
func TestScanAppLogRow_ScanError(t *testing.T) {
	rows := &mockAppLogRows{
		scanFn: func(dest ...any) error {
			return errors.New("scan error: wrong column count")
		},
		closeFn: func() {},
	}

	_, err := scanAppLogRow(rows)
	if err == nil {
		t.Fatal("expected error from scanAppLogRow when Scan fails, got nil")
	}
	if !strings.Contains(err.Error(), "scan error") {
		t.Errorf("expected scan error message, got %q", err.Error())
	}
}

// TestScanAppLogRow_Success tests that scanAppLogRow correctly maps
// database columns to AppLogEntry fields with proper UTC formatting.
func TestScanAppLogRow_Success(t *testing.T) {
	now := time.Now().UTC()
	catTime := now.Add(-time.Second)

	rows := &mockAppLogRows{
		scanFn: func(dest ...any) error {
			*(dest[0].(*string)) = "test-id-123"
			*(dest[1].(*time.Time)) = catTime
			*(dest[2].(*time.Time)) = now
			*(dest[3].(*string)) = "error"
			*(dest[4].(*string)) = "proxy"
			*(dest[5].(*string)) = "connection refused"
			return nil
		},
		closeFn: func() {},
	}

	entry, err := scanAppLogRow(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ID != "test-id-123" {
		t.Errorf("ID = %q, want %q", entry.ID, "test-id-123")
	}
	if entry.Level != "error" {
		t.Errorf("Level = %q, want %q", entry.Level, "error")
	}
	if entry.Source != "proxy" {
		t.Errorf("Source = %q, want %q", entry.Source, "proxy")
	}
	if entry.Message != "connection refused" {
		t.Errorf("Message = %q, want %q", entry.Message, "connection refused")
	}
	// Verify timestamps are formatted as RFC3339Nano in UTC
	if _, parseErr := time.Parse(time.RFC3339Nano, entry.CreatedAt); parseErr != nil {
		t.Errorf("CreatedAt is not valid RFC3339Nano: %q, error: %v", entry.CreatedAt, parseErr)
	}
	if _, parseErr := time.Parse(time.RFC3339Nano, entry.Timestamp); parseErr != nil {
		t.Errorf("Timestamp is not valid RFC3339Nano: %q, error: %v", entry.Timestamp, parseErr)
	}
}

// ---------------------------------------------------------------------------
// getAppLogsHistory query error path
// ---------------------------------------------------------------------------
