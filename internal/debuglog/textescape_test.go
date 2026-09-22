package debuglog

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// captureStdout runs fn with os.Stdout replaced by a pipe and returns whatever
// fn wrote to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prev }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}
	return out
}

// A caller-controlled value keeps its spaces and is quoted: plain logfmt, the
// shape every log reader parses and a human can read. The real client address
// stays the FIRST address-named token on the line, which is what the CrowdSec
// grok takes; a forged copy can only ever appear after it, inside quotes.
func TestStdoutHandler_TextQuotesValuesWithSpaces(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		logger := slog.New(StdoutHandler())
		logger.Warn("access: request",
			"method", "GET",
			"remote", "203.0.113.5",
			"status", 404,
			"path", "/x remote=198.51.100.9 y")
	})

	line := strings.TrimRight(out, "\n")
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("one record must be one line, got %q", out)
	}
	if !strings.Contains(line, ` remote=203.0.113.5 `) {
		t.Errorf("real client address not emitted bare; line: %s", line)
	}
	if !strings.Contains(line, `path="/x remote=198.51.100.9 y"`) {
		t.Errorf("caller value not quoted with its spaces intact; line: %s", line)
	}
	if strings.Contains(line, `\x20`) {
		t.Errorf("value was space-escaped; line: %s", line)
	}
	if i, j := strings.Index(line, "remote=203.0.113.5"), strings.Index(line, "remote=198.51.100.9"); i < 0 || j < i {
		t.Errorf("real address is not the first address token; line: %s", line)
	}
}

// The message carries the scope the parser classifies on and is not
// caller-controlled, so escaping must leave its spaces alone: mangling them
// would break every "scope: message" match in the collection.
func TestStdoutHandler_TextLeavesTheMessageIntact(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("auth: admin request with invalid token", "remote_addr", "203.0.113.5")
	})

	if !strings.Contains(out, `msg="auth: admin request with invalid token"`) {
		t.Errorf("message not emitted verbatim; line: %s", out)
	}
}

// An ordinary value has nothing to escape and must read exactly as before, so
// the common line stays human-readable and the existing fixtures keep matching.
func TestStdoutHandler_TextLeavesOrdinaryValuesBare(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("auth: CSRF check failed", "remote_addr", "203.0.113.5", "path", "/api/members")
	})

	if !strings.Contains(out, " remote_addr=203.0.113.5 path=/api/members") {
		t.Errorf("bare values were rewritten; line: %s", out)
	}
}

// A group expands into dotted keys, and a value inside one is quoted like any
// other rather than the whole group collapsing into one stringified attribute.
func TestStdoutHandler_TextKeepsGroupsExpanded(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("access: request", slog.Group("req", "path", "/a b"))
	})

	if !strings.Contains(out, `req.path="/a b"`) {
		t.Errorf("group not expanded with its values quoted; line: %s", out)
	}
}

// A timestamp renders as RFC3339 in slog's text handler, which holds no space
// at all. Rewriting it to a string would swap that for Go's sprawling default
// time format, so a typed value keeps its own rendering.
func TestStdoutHandler_TextLeavesTypedValuesAlone(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	when := time.Date(2026, 8, 18, 4, 15, 2, 123000000, time.UTC)
	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("frontdesk: stamp device last_seen", "when", when, "took", 1500*time.Millisecond)
	})

	if !strings.Contains(out, "when=2026-08-18T04:15:02.123Z") {
		t.Errorf("a time attribute lost its RFC3339 rendering; line: %s", out)
	}
	if !strings.Contains(out, "took=1.5s") {
		t.Errorf("a duration attribute lost its rendering; line: %s", out)
	}
}

// A backslash in the value is escaped by the quoting itself, so a reader that
// unquotes the value gets back exactly what the caller sent.
func TestStdoutHandler_TextQuotesBackslashesInValues(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("access: request", "remote", "203.0.113.5", "path", `/a\x20b c`)
	})

	if !strings.Contains(out, `path="/a\\x20b c"`) {
		t.Errorf("backslash not escaped by the quoting; line: %s", out)
	}
}
