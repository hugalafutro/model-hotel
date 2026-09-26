package debuglog

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
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

// Regression pin: the CrowdSec collection's Front Desk fixture
// (contrib/crowdsec/tests/model-hotel-logs) holds this exact line shape: the
// message quoted after msg=, the real client address the first address token,
// and a caller-controlled path quoted with its spaces intact. A change to
// StdoutHandler that renders it any other way stops that parser matching.
func TestStdoutHandler_WritesTheLineShapeTheCrowdSecFixtureHolds(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("DEBUG_LOG", "")
	Init()

	out := captureStdout(t, func() {
		slog.New(StdoutHandler()).Warn("auth: admin request with invalid token",
			"remote_addr", "203.0.113.27", "path", "/x remote_addr=203.0.113.77 y")
	})

	const want = ` level=WARN msg="auth: admin request with invalid token" remote_addr=203.0.113.27 path="/x remote_addr=203.0.113.77 y"` + "\n"
	if !strings.HasPrefix(out, "time=") || !strings.HasSuffix(out, want) || strings.Count(out, "\n") != 1 {
		t.Fatalf("line = %q, want time=... followed by %q", out, want)
	}
}
