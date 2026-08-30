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

// addressTokens returns the whitespace-delimited tokens of line that name a
// client address, which is exactly what the CrowdSec grok reads: it takes the
// FIRST such token on the line and validates it as an IP afterwards, and it
// drops the address entirely when a second one is present.
func addressTokens(line string) []string {
	var found []string
	for _, tok := range strings.Fields(line) {
		for _, key := range []string{"remote_addr=", "remote=", "client_ip=", "ip="} {
			if strings.HasPrefix(tok, key) {
				found = append(found, tok)
			}
		}
	}
	return found
}

// A caller-controlled attribute value must never be able to present a second
// "key=value" token to a log reader that splits on whitespace. The gateway's
// own handler escapes the spaces inside such a value (quoteLogValue in
// internal/api/applogs_slog.go); the stdout text handler that Front Desk runs
// on has to carry the same guarantee, or a request path is all it takes to
// name a stranger as the client of an authentication failure.
func TestStdoutHandler_TextEscapesSpacesInAttributeValues(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init(false)

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

	got := addressTokens(line)
	if len(got) != 1 {
		t.Fatalf("address tokens = %v (want exactly one, the real client), line: %s", got, line)
	}
	if got[0] != "remote=203.0.113.5" {
		t.Errorf("address token = %q, want %q; line: %s", got[0], "remote=203.0.113.5", line)
	}
}

// The message carries the scope the parser classifies on and is not
// caller-controlled, so escaping must leave its spaces alone: mangling them
// would break every "scope: message" match in the collection.
func TestStdoutHandler_TextLeavesTheMessageIntact(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init(false)

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
	Init(false)

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("auth: CSRF check failed", "remote_addr", "203.0.113.5", "path", "/api/members")
	})

	if !strings.Contains(out, " remote_addr=203.0.113.5 path=/api/members") {
		t.Errorf("bare values were rewritten; line: %s", out)
	}
}

// A group is a container, not a value: it keeps expanding into dotted keys and
// the escaping applies to the values inside it, rather than the whole group
// collapsing into one stringified attribute.
func TestStdoutHandler_TextKeepsGroupsExpanded(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init(false)

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("access: request", slog.Group("req", "path", "/a b"))
	})

	if !strings.Contains(out, `req.path=/a\x20b`) {
		t.Errorf("group not expanded with its values escaped; line: %s", out)
	}
}

// A timestamp renders as RFC3339 in slog's text handler, which holds no space
// at all. Rewriting it to a string would swap that for Go's sprawling default
// time format, so a typed value keeps its own rendering.
func TestStdoutHandler_TextLeavesTypedValuesAlone(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init(false)

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

// A backslash already in the value must not be able to masquerade as the
// escape this introduces, or a caller could write a literal "\x20" and have a
// reader that unescapes the value put the space back.
func TestStdoutHandler_TextEscapesBackslashesInEscapedValues(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init(false)

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("access: request", "remote", "203.0.113.5", "path", `/a\x20b c`)
	})

	if !strings.Contains(out, `path=/a\\x20b\x20c`) {
		t.Errorf("backslash not escaped ahead of the space escape; line: %s", out)
	}
}
