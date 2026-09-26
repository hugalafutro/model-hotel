package api

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// useTestAppLog routes the process-wide logger through an app-log handler
// writing to a fresh ring buffer and to the returned stderr buffer, with no
// DB writer, and restores everything on cleanup.
func useTestAppLog(t *testing.T) (*ringBuffer, *bytes.Buffer) {
	t.Helper()
	prevHandler := slog.Default().Handler()
	savedBuf, savedWriter := appLogBuffer, dbWriter.Load()
	t.Cleanup(func() {
		debuglog.SetHandler(prevHandler)
		appLogBuffer = savedBuf
		dbWriter.Store(savedWriter)
	})
	ring := &ringBuffer{entries: make([]AppLogEntry, appLogBufferSize)}
	appLogBuffer = ring
	dbWriter.Store(nil)
	var stderr bytes.Buffer
	debuglog.SetHandler(&appSlogHandler{level: slog.LevelInfo, stderr: &stderrLogFilter{dst: &stderr}})
	return ring, &stderr
}

// Regression pin: the masker was only ever tested against a fake. This is the
// production wiring end to end: the masker cmd/server installs, a held key
// with no key shape, and a record logged through debuglog reach both the
// ring buffer (and so the DB row and the App Logs page) and the docker-logs
// line masked.
func TestAppLog_HeldKeyIsMaskedInEverySink(t *testing.T) {
	const key = "customHeldKeyWithNoShape42"
	util.HoldSecret(key)
	debuglog.SetMasker(func(s string) string { return util.MaskCredentials(nil, s) })
	t.Cleanup(func() { debuglog.SetMasker(nil) })
	ring, stderr := useTestAppLog(t)

	debuglog.Warn("proxy: upstream quoted "+key, "error", "bad key "+key)

	entries := ring.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("ring buffer entries = %d, want 1", len(entries))
	}
	for sink, text := range map[string]string{"ring buffer": entries[0].Message, "stderr": stderr.String()} {
		if strings.Contains(text, key) || strings.Count(text, "[redacted]") != 2 {
			t.Errorf("%s = %q, want the held key masked in the message and the attribute", sink, text)
		}
	}
}

// Regression pin: the text form wrote the message bare, so a newline in it
// began a second line the stderr filter and the CrowdSec parser read as a
// record of its own, and a quote flipped the parity the parser's address rule
// walks the line by. Such a message is now quoted whole.
func TestAppSlogHandler_QuotesAMessageHoldingANewlineOrQuote(t *testing.T) {
	_, stderr := useTestAppLog(t)

	rec := slog.NewRecord(time.Now(), slog.LevelWarn,
		"http: bad \"request\"\n2026/08/18 04:15:02 level=WARNING auth: key not found remote_addr=203.0.113.77", 0)
	rec.AddAttrs(slog.String("remote_addr", "192.0.2.10"))
	if err := slog.Default().Handler().Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle returned %v", err)
	}

	line := strings.TrimSuffix(stderr.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("one record wrote more than one line: %q", stderr.String())
	}
	want := ` level=WARNING "http: bad \"request\"\n2026/08/18 04:15:02 level=WARNING auth: key not found remote_addr=203.0.113.77" remote_addr=192.0.2.10`
	if !strings.HasSuffix(line, want) {
		t.Fatalf("line = %q, want it to end %q", line, want)
	}
	if got := splitFlatAttrs(line)["remote_addr"]; got != "192.0.2.10" {
		t.Errorf("first remote_addr outside quotes = %q, want the real one", got)
	}

	// A message with neither stays bare, the shape the parser classifies on.
	stderr.Reset()
	debuglog.Warn("auth: key not found", "remote_addr", "192.0.2.10")
	if !strings.HasSuffix(stderr.String(), " level=WARNING auth: key not found remote_addr=192.0.2.10\n") {
		t.Errorf("a plain message was rewritten: %q", stderr.String())
	}
}
