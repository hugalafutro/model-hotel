package api

import (
	"testing"
)

// ---------------------------------------------------------------------------
// stripLevelPrefix
// ---------------------------------------------------------------------------

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

func TestStripLevelPrefix_DEBUGNotStripped(t *testing.T) {
	result := stripLevelPrefix("DEBUG something")
	if result != "DEBUG something" {
		t.Errorf("DEBUG prefix should not be stripped, got %q", result)
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
// extractSource
// ---------------------------------------------------------------------------

func TestExtractSource_WithSource(t *testing.T) {
	source, msg := extractSource("[proxy] request received")
	if source != "proxy" {
		t.Errorf("expected source %q, got %q", "proxy", source)
	}
	if msg != "request received" {
		t.Errorf("expected message %q, got %q", "request received", msg)
	}
}

func TestExtractSource_NoSource(t *testing.T) {
	source, msg := extractSource("just a message")
	if source != "" {
		t.Errorf("expected empty source, got %q", source)
	}
	if msg != "just a message" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestExtractSource_EmptyBrackets(t *testing.T) {
	// "[] " has empty source and no trailing space after ]...
	// actually end=1, line[end+1]!= ' ' so this won't extract
	source, _ := extractSource("[] something")
	if source != "" {
		t.Errorf("expected empty source for empty brackets, got %q", source)
	}
}

func TestExtractSource_NoSpaceAfterBracket(t *testing.T) {
	source, _ := extractSource("[proxy]no space")
	if source != "" {
		t.Errorf("expected empty source when no space after ], got %q", source)
	}
}

// ---------------------------------------------------------------------------
// detectLevel
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

func TestDetectLevel_Info(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"normal log", "[proxy] request processed"},
		{"INFO prefix", "INFO  something happened"},
		{"debug word", "debug: tracing"},
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
