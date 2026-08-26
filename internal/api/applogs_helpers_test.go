package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The pure log-line helpers: level-prefix stripping, timestamp stripping,
// the after-filter and the word matcher.

func TestStripLevelPrefix_INFO(t *testing.T) {
	result := stripLevelPrefix("INFO  hello world")
	if result != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", result)
	}
}

func TestStripLevelPrefix_WARN(t *testing.T) {
	result := stripLevelPrefix("WARN  something happened")
	if result != "something happened" {
		t.Errorf("expected %q, got %q", "something happened", result)
	}
}

func TestStripLevelPrefix_ERROR(t *testing.T) {
	result := stripLevelPrefix("ERROR failed to connect")
	if result != "failed to connect" {
		t.Errorf("expected %q, got %q", "failed to connect", result)
	}
}

func TestStripLevelPrefix_NoPrefix(t *testing.T) {
	result := stripLevelPrefix("just a message")
	if result != "just a message" {
		t.Errorf("expected %q, got %q", "just a message", result)
	}
}

func TestStripLevelPrefix_EmptyString(t *testing.T) {
	result := stripLevelPrefix("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestStripLevelPrefix_INFOWithoutSpaces(t *testing.T) {
	// "INFO " (5 chars) won't match "INFO " prefix — requires "INFO  " with 2 spaces
	result := stripLevelPrefix("INFO hello")
	if result != "INFO hello" {
		t.Errorf("INFO with single space should not strip, got %q", result)
	}
}

func TestStripLevelPrefix_DEBUG(t *testing.T) {
	result := stripLevelPrefix("DEBUG  something")
	if result != "something" {
		t.Errorf("expected %q, got %q", "something", result)
	}
}

func TestStripLevelPrefix_DEBUGWithoutSpaces(t *testing.T) {
	result := stripLevelPrefix("DEBUG something")
	if result != "DEBUG something" {
		t.Errorf("DEBUG with single space should not strip, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// filterEntriesAfter
// ---------------------------------------------------------------------------

func TestFilterEntriesAfter_BasicFiltering(t *testing.T) {
	after := "2024-01-01T12:00:00Z"
	entries := []AppLogEntry{
		{Timestamp: "2024-01-01T11:00:00.000000000Z", Level: "info", Message: "before"},
		{Timestamp: "2024-01-01T12:30:00.000000000Z", Level: "info", Message: "after"},
		{Timestamp: "2024-01-01T13:00:00.000000000Z", Level: "info", Message: "later"},
	}

	result := filterEntriesAfter(entries, after)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries after filter, got %d", len(result))
	}
	if result[0].Message != "after" {
		t.Errorf("expected first entry %q, got %q", "after", result[0].Message)
	}
}

func TestFilterEntriesAfter_RFC3339Nano(t *testing.T) {
	after := "2024-01-01T12:00:00.123456789Z"
	entries := []AppLogEntry{
		{Timestamp: "2024-01-01T11:59:59.999999999Z", Message: "before"},
		{Timestamp: "2024-01-01T12:00:01.000000000Z", Message: "after"},
	}

	result := filterEntriesAfter(entries, after)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Message != "after" {
		t.Errorf("expected %q, got %q", "after", result[0].Message)
	}
}

func TestFilterEntriesAfter_AllBefore(t *testing.T) {
	after := "2024-01-01T15:00:00Z"
	entries := []AppLogEntry{
		{Timestamp: "2024-01-01T10:00:00Z", Message: "first"},
		{Timestamp: "2024-01-01T12:00:00Z", Message: "second"},
	}

	result := filterEntriesAfter(entries, after)
	if result != nil {
		t.Errorf("expected nil for all entries before threshold, got %d entries", len(result))
	}
}

func TestFilterEntriesAfter_InvalidAfter(t *testing.T) {
	// On parse failure, returns original slice
	entries := []AppLogEntry{
		{Timestamp: "2024-01-01T10:00:00Z", Message: "entry"},
	}

	result := filterEntriesAfter(entries, "not-a-timestamp")
	if len(result) != 1 {
		t.Errorf("invalid after should return original slice, got %d entries", len(result))
	}
}

func TestFilterEntriesAfter_EmptyAfter(t *testing.T) {
	entries := []AppLogEntry{
		{Timestamp: "2024-01-01T10:00:00Z", Message: "entry"},
	}

	result := filterEntriesAfter(entries, "")
	if len(result) != 1 {
		t.Errorf("empty after should trigger parse failure and return original, got %d entries", len(result))
	}
}

func TestFilterEntriesAfter_EmptyEntries(t *testing.T) {
	result := filterEntriesAfter(nil, "2024-01-01T12:00:00Z")
	if result != nil {
		t.Errorf("expected nil for nil entries, got %d entries", len(result))
	}
}

func TestFilterEntriesAfter_ExactTimestamp(t *testing.T) {
	// filterEntriesAfter uses strict After(), so equal timestamps should be excluded
	ts := "2024-01-01T12:00:00.000000000Z"
	entries := []AppLogEntry{
		{Timestamp: ts, Message: "exact"},
		{Timestamp: "2024-01-01T12:00:01.000000000Z", Message: "later"},
	}

	result := filterEntriesAfter(entries, ts)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (strict after), got %d", len(result))
	}
	if result[0].Message != "later" {
		t.Errorf("expected %q, got %q", "later", result[0].Message)
	}
}

// ---------------------------------------------------------------------------
// stripLogTimestamp
// ---------------------------------------------------------------------------

func TestStripLogTimestamp_WithTimestamp(t *testing.T) {
	line := "2024/01/15 09:30:00 [proxy] request received"
	result := stripLogTimestamp(line)
	expected := "[proxy] request received"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestStripLogTimestamp_NoTimestamp(t *testing.T) {
	line := "just a message"
	result := stripLogTimestamp(line)
	if result != line {
		t.Errorf("expected %q, got %q", line, result)
	}
}

func TestStripLogTimestamp_ShortLine(t *testing.T) {
	line := "short"
	result := stripLogTimestamp(line)
	if result != "short" {
		t.Errorf("lines shorter than 20 chars should be returned unchanged, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// stripLevelPrefix key=value format
// ---------------------------------------------------------------------------

func TestStripLevelPrefix_LevelEqualsInfo(t *testing.T) {
	result := stripLevelPrefix("level=INFO request completed")
	if result != "request completed" {
		t.Errorf("expected %q, got %q", "request completed", result)
	}
}

func TestStripLevelPrefix_LevelEqualsWarn(t *testing.T) {
	result := stripLevelPrefix("level=WARN slow response")
	if result != "slow response" {
		t.Errorf("expected %q, got %q", "slow response", result)
	}
}

func TestStripLevelPrefix_LevelEqualsError(t *testing.T) {
	result := stripLevelPrefix("level=ERROR connection refused")
	if result != "connection refused" {
		t.Errorf("expected %q, got %q", "connection refused", result)
	}
}

func TestStripLevelPrefix_LevelEqualsDebug(t *testing.T) {
	result := stripLevelPrefix("level=DEBUG trace output")
	if result != "trace output" {
		t.Errorf("expected %q, got %q", "trace output", result)
	}
}

// ---------------------------------------------------------------------------
// detectLevel
// ---------------------------------------------------------------------------

func TestWordMatch_Basic(t *testing.T) {
	tests := []struct {
		s      string
		word   string
		result bool
	}{
		{"error", "error", true},
		{"an error occurred", "error", true},
		{"error: bad thing", "error", true},
		{"error_chunks=0", "error", false},
		{"has_error=false", "error", false},
		{"errorHandling", "error", false},
		{"no issues here", "error", false},
		{"warn: something", "warn", true},
		{"warning: deprecated", "warn", false},
		{"warning: deprecated", "warning", true},
		{"warnings were present", "warn", false},
		{"warnings were present", "warning", false}, // "warning" doesn't match "warnings" (trailing s)
		{"warnings were present", "warnings", true}, // "warnings" as exact word does match
		{"warning: check this", "warning", true},    // "warning" as exact word does match
		{"has_warnings=true", "warn", false},
		{"has_warnings=true", "warning", false}, // "warnings" preceded by _, not word boundary
		{"fatal error", "fatal", true},
		{"fatality", "fatal", false},
		{"panic: crashed", "panic", true},
		{"panicking", "panic", false},
		// Word at start and end of string
		{"error at start", "error", true},
		{"at end error", "error", true},
		// Punctuation boundaries
		{"error, something", "error", true},
		{"error.", "error", true},
		{"error=bad_thing", "error", true}, // "error" as whole word before =
	}
	for _, tc := range tests {
		t.Run(tc.s+"/"+tc.word, func(t *testing.T) {
			got := wordMatch(strings.ToLower(tc.s), tc.word)
			if got != tc.result {
				t.Errorf("wordMatch(%q, %q) = %v, want %v", tc.s, tc.word, got, tc.result)
			}
		})
	}
}

type errWriterMock struct{}

func (errWriterMock) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write error")
}

// ---------------------------------------------------------------------------
// GetAppLogsCursor Tests
// ---------------------------------------------------------------------------

// mockAppLogRows implements pgx.Rows for testing scanAppLogRow error paths.
type mockAppLogRows struct {
	scanFn  func(dest ...any) error
	closeFn func()
}

func (m *mockAppLogRows) Close()                        { m.closeFn() }
func (m *mockAppLogRows) Err() error                    { return nil }
func (m *mockAppLogRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("") }
func (m *mockAppLogRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (m *mockAppLogRows) Next() bool             { return false }
func (m *mockAppLogRows) Scan(dest ...any) error { return m.scanFn(dest...) }
func (m *mockAppLogRows) Values() ([]any, error) { return nil, nil }
func (m *mockAppLogRows) RawValues() [][]byte    { return nil }
func (m *mockAppLogRows) Conn() *pgx.Conn        { return nil }
