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
// while loopback http (a secure context for WebAuthn) stays allowed for local use.
func TestNewRelyingPartyEnforcesHTTPS(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://frontdesk.example.com", true},
		{"https://frontdesk.example.com:8443", true},
		{"http://frontdesk.example.com", false}, // plain http is rejected
		{"http://localhost:8090", true},         // loopback http allowed
		{"http://127.0.0.1:8090", true},
		{"http://[::1]:8090", true},
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
