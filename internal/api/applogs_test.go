package api

import (
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
	"github.com/jackc/pgx/v5/pgxpool"
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
// extractSource colon-separated format
// ---------------------------------------------------------------------------

func TestExtractSource_ColonSimple(t *testing.T) {
	source, msg := extractSource("proxy: request received")
	if source != "proxy" {
		t.Errorf("expected source %q, got %q", "proxy", source)
	}
	if msg != "request received" {
		t.Errorf("expected message %q, got %q", "request received", msg)
	}
}

func TestExtractSource_ColonHyphenated(t *testing.T) {
	source, msg := extractSource("circuit-breaker: provider state=open")
	if source != "circuit-breaker" {
		t.Errorf("expected source %q, got %q", "circuit-breaker", source)
	}
	if msg != "provider state=open" {
		t.Errorf("expected message %q, got %q", "provider state=open", msg)
	}
}

func TestExtractSource_ColonWithDots(t *testing.T) {
	source, msg := extractSource("models.dev: loaded models")
	if source != "models.dev" {
		t.Errorf("expected source %q, got %q", "models.dev", source)
	}
	if msg != "loaded models" {
		t.Errorf("expected message %q, got %q", "loaded models", msg)
	}
}

func TestExtractSource_ColonWithUnderscore(t *testing.T) {
	source, msg := extractSource("TRUSTED_PROXIES: skipping invalid CIDR")
	if source != "TRUSTED_PROXIES" {
		t.Errorf("expected source %q, got %q", "TRUSTED_PROXIES", source)
	}
	if msg != "skipping invalid CIDR" {
		t.Errorf("expected message %q, got %q", "skipping invalid CIDR", msg)
	}
}

func TestExtractSource_ColonStartsWithDigit(t *testing.T) {
	source, msg := extractSource("1invalid: message")
	if source != "" {
		t.Errorf("expected empty source for digit-start, got %q", source)
	}
	if msg != "1invalid: message" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestExtractSource_ColonSpaceInCandidate(t *testing.T) {
	source, msg := extractSource("foo bar: message")
	if source != "" {
		t.Errorf("expected empty source for space in candidate, got %q", source)
	}
	if msg != "foo bar: message" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestExtractSource_ColonSingleChar(t *testing.T) {
	// Single-char source before colon is too short (needs >= 2 chars)
	source, msg := extractSource("a: message")
	if source != "" {
		t.Errorf("expected empty source for single-char, got %q", source)
	}
	if msg != "a: message" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestExtractSource_ColonSpecialChars(t *testing.T) {
	source, msg := extractSource("hello@world: message")
	if source != "" {
		t.Errorf("expected empty source for special chars, got %q", source)
	}
	if msg != "hello@world: message" {
		t.Errorf("expected unchanged message, got %q", msg)
	}
}

func TestExtractSource_BracketPreferredOverColon(t *testing.T) {
	// Bracketed format should be tried first
	source, msg := extractSource("[proxy] access: request")
	if source != "proxy" {
		t.Errorf("expected source %q, got %q", "proxy", source)
	}
	if msg != "access: request" {
		t.Errorf("expected message %q, got %q", "access: request", msg)
	}
}

func TestExtractSource_ColonOpencodeGo(t *testing.T) {
	source, msg := extractSource("opencode-go: discovered models")
	if source != "opencode-go" {
		t.Errorf("expected source %q, got %q", "opencode-go", source)
	}
	if msg != "discovered models" {
		t.Errorf("expected message %q, got %q", "discovered models", msg)
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

func TestGetAppLogsHistory_NilDBPool(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp appLogsHistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(resp.Entries))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestGetAppLogsHistory_NilDBPool_JSONEncodeError(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	w := &brokenResponseWriter{header: make(http.Header)}

	// Should not panic, just log the error
	h.getAppLogsHistory(w, req)
}

func TestGetAppLogsHistory_InvalidPage(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&page=0", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_InvalidPerPage(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&per_page=200", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_ToParam(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&to=2024-12-31T23:59:59Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_SortByAndDir(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true&sort_by=time&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestGetAppLogsHistory_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs/app?history=true", http.NoBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The handler returns an error message in the body (status 200)
	// Note: handler doesn't set 500 status, just returns error JSON
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	// Verify error response is returned
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestGetAppLogCounts_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
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
		t.Skip("apiTestDBURL not set, skipping integration test")
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
	for i := 0; i < 50; i++ {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "test",
			Message:   fmt.Sprintf("batch entry %d", i),
		}
	}

	// Give the goroutine time to process
	time.Sleep(200 * time.Millisecond)

	// Verify entries were written to DB
	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs WHERE source = 'test'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query app_logs: %v", err)
	}
	if count < 50 {
		t.Errorf("expected at least 50 entries in DB, got %d", count)
	}
}

func TestDBLogWriter_TickerFlush(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
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
	for i := 0; i < 5; i++ {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "ticker-test",
			Message:   fmt.Sprintf("ticker entry %d", i),
		}
	}

	// Wait for the 500ms ticker to fire and flush (lines 131-135)
	time.Sleep(800 * time.Millisecond)

	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs WHERE source = 'ticker-test'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query app_logs: %v", err)
	}
	if count < 5 {
		t.Errorf("expected at least 5 entries in DB after ticker flush, got %d", count)
	}
}

func TestDBLogWriter_FlushDBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}
	// Create a writer with a closed pool to trigger the Exec error path (lines 160-164)
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close() // Close immediately to cause DB errors

	w := newDBLogWriter(pool)
	defer w.stop()

	// Send entries — they'll be flushed but the DB write will fail silently
	for i := 0; i < 5; i++ {
		w.ch <- AppLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Source:    "flush-error-test",
			Message:   fmt.Sprintf("entry %d", i),
		}
	}

	// Wait for ticker flush (the batch is small, so ticker will flush it)
	time.Sleep(800 * time.Millisecond)

	// No panic or hang means the error was handled gracefully
}

func TestRingBuffer_WriteWithDBWriter(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

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
	time.Sleep(800 * time.Millisecond)

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

type errWriterMock struct{}

func (errWriterMock) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write error")
}

// ---------------------------------------------------------------------------
// GetAppLogsCursor Tests
// ---------------------------------------------------------------------------

func TestGetAppLogsCursor_Default(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with different timestamp and created_at values
	for i := 0; i < 5; i++ {
		logID := uuid.New().String()
		eventTs := time.Now().Add(-time.Duration(i) * time.Minute).UTC()
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			logID,
			eventTs.Format(time.RFC3339Nano),
			"info",
			"test",
			fmt.Sprintf("test message %d", i),
			createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Test default cursor request (no cursor)
	req := httptest.NewRequest("GET", "/logs/app/cursor", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppLogsCursorResponse
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
	// Verify level_counts and source_counts are present
	if resp.LevelCounts == nil {
		t.Error("expected LevelCounts to be non-nil")
	}
	if resp.SourceCounts == nil {
		t.Error("expected SourceCounts to be non-nil")
	}
}

func TestGetAppLogsCursor_WithCursor(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with distinct timestamps (1 day apart)
	// Use different values for timestamp (event time) and created_at (insertion time)
	// to ensure cursor pagination uses created_at, not timestamp
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		logID := uuid.New().String()
		eventTs := now.Add(-time.Duration(i) * 24 * time.Hour)
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			logID,
			eventTs.Format(time.RFC3339Nano),
			"info",
			"test",
			fmt.Sprintf("test message %d", i),
			createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// First request to get initial page
	req := httptest.NewRequest("GET", "/logs/app/cursor?limit=2", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var firstResp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	if len(firstResp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(firstResp.Entries))
	}
	if firstResp.HasBefore {
		t.Error("expected HasBefore=false for first page (no cursor)")
	}

	// Build a cursor from the last entry's created_at (insertion time, not event timestamp)
	lastEntry := firstResp.Entries[len(firstResp.Entries)-1]
	cursorCat, err := time.Parse(time.RFC3339Nano, lastEntry.CreatedAt)
	if err != nil {
		t.Fatalf("failed to parse cursor created_at: %v", err)
	}
	cursor := appLogCursor{
		CreatedAt: cursorCat,
		ID:        lastEntry.ID,
	}
	cursorStr := cursor.encode()

	// Second request with cursor - verify has_before is set
	req = httptest.NewRequest("GET", "/logs/app/cursor?cursor="+url.QueryEscape(cursorStr)+"&limit=2", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var secondResp AppLogsCursorResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}

	// Key assertion: has_before should be true when cursor is provided
	if !secondResp.HasBefore {
		t.Error("expected HasBefore=true when using cursor")
	}
	// Response should still have valid structure
	if secondResp.LevelCounts == nil {
		t.Error("expected LevelCounts to be non-nil")
	}
	if secondResp.SourceCounts == nil {
		t.Error("expected SourceCounts to be non-nil")
	}
}

func TestGetAppLogsCursor_InvalidCursor(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

	_, r := newTestHandlerWithRouter(t)

	// Test with invalid base64 cursor
	req := httptest.NewRequest("GET", "/logs/app/cursor?cursor=not-valid-base64", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid cursor, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		// respondBadRequest returns plain text, not JSON
		if w.Body.String() == "" {
			t.Error("expected error message for invalid cursor")
		}
	} else if resp["error"] == "" && resp["message"] == "" {
		t.Error("expected error message for invalid cursor")
	}
}

func TestGetAppLogsCursor_WithFilters(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	// Insert test app logs with different levels and sources
	testCases := []struct {
		level  string
		source string
		msg    string
	}{
		{"info", "proxy", "proxy info message"},
		{"warning", "auth", "auth warning message"},
		{"error", "proxy", "proxy error message"},
		{"info", "discovery", "discovery info message"},
	}

	for _, tc := range testCases {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW())`,
			uuid.New().String(),
			time.Now().UTC().Format(time.RFC3339Nano),
			tc.level,
			tc.source,
			tc.msg)
		if err != nil {
			t.Fatalf("Failed to insert app log: %v", err)
		}
	}

	// Test level filter
	req := httptest.NewRequest("GET", "/logs/app/cursor?level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("level filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.Level != "error" {
			t.Errorf("expected level 'error', got %q", entry.Level)
		}
	}

	// Test source filter
	req = httptest.NewRequest("GET", "/logs/app/cursor?source=proxy", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("source filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.Source != "proxy" {
			t.Errorf("expected source 'proxy', got %q", entry.Source)
		}
	}
}

// TestGetAppLogsCursor_BackwardPagination tests that direction=before returns
// the items immediately preceding the cursor, not items from the start of
// the dataset, and that results are in the requested sort order.
func TestGetAppLogsCursor_BackwardPagination(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("apiTestDBURL not set, skipping integration test")
	}

	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()

	now := time.Now().UTC()
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = uuid.New().String()
		eventTs := now.Add(-time.Duration(i) * time.Hour)
		createdAt := eventTs.Add(time.Duration(i) * time.Second)
		_, err := pool.Exec(context.Background(),
			`INSERT INTO app_logs (id, timestamp, level, source, message, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			ids[i], eventTs.Format(time.RFC3339Nano), "info", "test",
			fmt.Sprintf("backward-msg-%d", i), createdAt)
		if err != nil {
			t.Fatalf("Failed to insert app log %d: %v", i, err)
		}
	}

	// Page 1 DESC (newest 3)
	req := httptest.NewRequest("GET", "/logs/app/cursor?limit=3&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("failed to decode page1: %v", err)
	}
	if len(page1.Entries) != 3 {
		t.Fatalf("expected 3 entries on page1, got %d", len(page1.Entries))
	}

	// Page 2
	page1Last := page1.Entries[len(page1.Entries)-1]
	cursor1Cat, _ := time.Parse(time.RFC3339Nano, page1Last.CreatedAt)
	cursor1 := appLogCursor{CreatedAt: cursor1Cat, ID: page1Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor1.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page2: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("failed to decode page2: %v", err)
	}
	if len(page2.Entries) != 3 {
		t.Fatalf("expected 3 entries on page2, got %d", len(page2.Entries))
	}

	// Page 3
	page2Last := page2.Entries[len(page2.Entries)-1]
	cursor2Cat, _ := time.Parse(time.RFC3339Nano, page2Last.CreatedAt)
	cursor2 := appLogCursor{CreatedAt: cursor2Cat, ID: page2Last.ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=after", url.QueryEscape(cursor2.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("page3: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page3 AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page3); err != nil {
		t.Fatalf("failed to decode page3: %v", err)
	}
	if len(page3.Entries) != 3 {
		t.Fatalf("expected 3 entries on page3, got %d", len(page3.Entries))
	}

	// Backward from page3's first entry — should return page2's entries
	backwardCat, _ := time.Parse(time.RFC3339Nano, page3.Entries[0].CreatedAt)
	backwardCursor := appLogCursor{CreatedAt: backwardCat, ID: page3.Entries[0].ID}
	req = httptest.NewRequest("GET", fmt.Sprintf("/logs/app/cursor?limit=3&sort_dir=desc&cursor=%s&direction=before", url.QueryEscape(backwardCursor.encode())), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("backward page: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var beforePage AppLogsCursorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &beforePage); err != nil {
		t.Fatalf("failed to decode backward page: %v", err)
	}

	if len(beforePage.Entries) != 3 {
		t.Fatalf("expected 3 entries for backward page, got %d", len(beforePage.Entries))
	}

	// Results must match page2 entries (DESC order)
	if beforePage.Entries[0].ID != page2.Entries[0].ID {
		t.Errorf("expected first entry ID %s, got %s", page2.Entries[0].ID, beforePage.Entries[0].ID)
	}
	if beforePage.Entries[1].ID != page2.Entries[1].ID {
		t.Errorf("expected second entry ID %s, got %s", page2.Entries[1].ID, beforePage.Entries[1].ID)
	}
	if beforePage.Entries[2].ID != page2.Entries[2].ID {
		t.Errorf("expected third entry ID %s, got %s", page2.Entries[2].ID, beforePage.Entries[2].ID)
	}

	if !beforePage.HasAfter {
		t.Error("expected HasAfter=true for backward page with cursor")
	}
	if !beforePage.HasBefore {
		t.Error("expected HasBefore=true for backward page (more items precede)")
	}
}
