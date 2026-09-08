package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// TestNewRelyingPartyEnforcesHTTPS pins the HTTPS-only ingress guarantee: a
// plain-http PUBLIC_ORIGIN is refused so a misconfigured deploy fails loudly,
// while http://localhost (a secure context for WebAuthn) stays allowed for
// local use. An IP literal is refused on any scheme: a WebAuthn relying party
// ID must be a domain, so no passkey could ever be minted for it.
func TestNewRelyingPartyEnforcesHTTPS(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://frontdesk.example.com", true},
		{"https://frontdesk.example.com:8443", true},
		{"http://frontdesk.example.com", false}, // plain http is rejected
		{"http://localhost:8090", true},         // localhost http allowed
		{"http://127.0.0.1:8090", false},        // an IP literal is never a valid RP ID
		{"http://[::1]:8090", false},
		{"https://127.0.0.1:8443", false}, // not even over https
		{"ftp://frontdesk.example.com", false},
		{"", false},
		{"https://", false}, // no host
	}
	for _, c := range cases {
		_, err := newRelyingParty(c.origin)
		if c.ok && err != nil {
			t.Errorf("newRelyingParty(%q) = %v, want success", c.origin, err)
		}
		if !c.ok && err == nil {
			t.Errorf("newRelyingParty(%q) = nil, want an error", c.origin)
		}
	}
}

// recordingHandler captures log records so the boot-time warning can be
// asserted without parsing stdout.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// A short FRONTDESK_MASTER_KEY is warned about at boot, like the main server's
// MASTER_KEY; a generator-length key is silent. Warn only: the process must
// still start, since rotating the key would orphan everything encrypted.
func TestWarnWeakMasterKey(t *testing.T) {
	rec := &recordingHandler{}
	prev := slog.Default()
	debuglog.SetHandler(rec)
	t.Cleanup(func() { slog.SetDefault(prev) })

	warnWeakMasterKey("hunter2")
	if n := len(rec.records); n != 1 {
		t.Fatalf("short key: %d records, want 1 warning", n)
	}
	r := rec.records[0]
	if r.Level != slog.LevelWarn || !strings.Contains(r.Message, "FRONTDESK_MASTER_KEY is shorter than recommended") {
		t.Errorf("short key: got %v %q", r.Level, r.Message)
	}
	if strings.Contains(r.Message, "hunter2") {
		t.Error("the warning must not echo the key")
	}

	rec.records = nil
	warnWeakMasterKey(strings.Repeat("k", config.RecommendedMasterKeyLength))
	if len(rec.records) != 0 {
		t.Errorf("strong key: unexpected log %q", rec.records[0].Message)
	}
}

// TestGeneratedTokenNeverReachesTheStructuredLogger is the whole point of
// printing the token to stdout directly.
//
// debuglog's handler fans out to every configured channel: the JSON stdout
// handler, where the token becomes an indexed queryable field, and the OTLP
// exporter, which ships it to whatever log store the operator runs and retains
// it there. A one-time login credential must not be persisted in log
// infrastructure. The gateway has always printed its own token straight to
// stdout for this reason; Front Desk logged it as a structured attribute.
func TestGeneratedTokenNeverReachesTheStructuredLogger(t *testing.T) {
	const token = "fd-secret-token-do-not-log"

	logs := &recordingHandler{}
	prev := slog.Default()
	debuglog.SetHandler(logs)
	t.Cleanup(func() { slog.SetDefault(prev) })

	var out strings.Builder
	announceGeneratedToken(&out, token)

	if !strings.Contains(out.String(), token) {
		t.Errorf("the token must still reach stdout so the operator can capture it; got %q", out.String())
	}

	logs.mu.Lock()
	defer logs.mu.Unlock()
	for _, r := range logs.records {
		if strings.Contains(r.Message, token) {
			t.Errorf("token leaked into a log message: %q", r.Message)
		}
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), token) {
				t.Errorf("token leaked into log attribute %q", a.Key)
			}
			return true
		})
	}
	if len(logs.records) == 0 {
		t.Error("expected a (token-free) log line noting that a token was generated")
	}
}
