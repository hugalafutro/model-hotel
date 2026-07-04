package debuglog

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// captureHandler captures log records for testing
type captureHandler struct {
	records []slog.Record
	level   slog.Level
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func newCaptureHandler(level slog.Level) *captureHandler {
	return &captureHandler{level: level}
}

func TestStdoutHandler(t *testing.T) {
	t.Run("text handler when LOG_FORMAT unset", func(t *testing.T) {
		t.Setenv("LOG_FORMAT", "")
		if _, ok := StdoutHandler().(*slog.TextHandler); !ok {
			t.Errorf("StdoutHandler() = %T, want *slog.TextHandler", StdoutHandler())
		}
	})

	t.Run("json handler when LOG_FORMAT=json", func(t *testing.T) {
		t.Setenv("LOG_FORMAT", "json")
		if _, ok := StdoutHandler().(*slog.JSONHandler); !ok {
			t.Errorf("StdoutHandler() = %T, want *slog.JSONHandler", StdoutHandler())
		}
	})

	t.Run("honours the level set by Init", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "")
		Init(true)
		if !StdoutHandler().Enabled(context.Background(), slog.LevelDebug) {
			t.Errorf("StdoutHandler() should accept Debug after Init(true)")
		}
	})
}

func TestInit(t *testing.T) {
	t.Run("debug true sets LevelDebug", func(t *testing.T) {
		Init(true)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})

	t.Run("debug false with no env sets LevelInfo", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "")
		Init(false)
		if got := Level(); got != slog.LevelInfo {
			t.Errorf("Level() = %v, want %v", got, slog.LevelInfo)
		}
	})

	t.Run("debug false with DEBUG_LOG=true still sets LevelDebug", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "true")
		Init(false)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})
}

func TestIsDebugLogEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"true", "true", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"TRUE", "TRUE", true},
		{"Yes", "Yes", true},
		{"empty", "", false},
		{"false", "false", false},
		{"0", "0", false},
		{"no", "no", false},
		{"random", "maybe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEBUG_LOG", tt.env)
			if got := isDebugLogEnv(); got != tt.want {
				t.Errorf("isDebugLogEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	t.Run("after Init(true) returns Debug", func(t *testing.T) {
		Init(true)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})

	t.Run("after Init(false) returns Info", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "")
		Init(false)
		if got := Level(); got != slog.LevelInfo {
			t.Errorf("Level() = %v, want %v", got, slog.LevelInfo)
		}
	})
}

func TestSetHandler(t *testing.T) {
	h := newCaptureHandler(slog.LevelInfo)
	SetHandler(h)

	// Verify handler is used by logging something
	Info("test message")
	if len(h.records) == 0 {
		t.Error("SetHandler: custom handler not being used")
	}
}

func TestDebug(t *testing.T) {
	t.Run("when debug enabled", func(t *testing.T) {
		Init(true)
		h := newCaptureHandler(slog.LevelDebug)
		SetHandler(h)

		Debug("debug message", "key", "value")
		if len(h.records) == 0 {
			t.Error("Debug: no record captured")
		}
		rec := h.records[0]
		if rec.Level != slog.LevelDebug {
			t.Errorf("Debug: level = %v, want %v", rec.Level, slog.LevelDebug)
		}
		if rec.Message != "debug message" {
			t.Errorf("Debug: message = %q, want %q", rec.Message, "debug message")
		}
	})

	t.Run("when debug disabled", func(t *testing.T) {
		Init(false)
		h := newCaptureHandler(slog.LevelInfo)
		SetHandler(h)

		Debug("debug message")
		// When debug is disabled, Debug calls should be discarded by slog
		if len(h.records) > 0 {
			t.Error("Debug: should not capture records when debug disabled")
		}
	})
}

func TestInfo(t *testing.T) {
	Init(true)
	h := newCaptureHandler(slog.LevelInfo)
	SetHandler(h)

	Info("info message", "key", "value")
	if len(h.records) == 0 {
		t.Error("Info: no record captured")
	}
	rec := h.records[0]
	if rec.Level != slog.LevelInfo {
		t.Errorf("Info: level = %v, want %v", rec.Level, slog.LevelInfo)
	}
	if rec.Message != "info message" {
		t.Errorf("Info: message = %q, want %q", rec.Message, "info message")
	}
}

func TestWarn(t *testing.T) {
	Init(true)
	h := newCaptureHandler(slog.LevelWarn)
	SetHandler(h)

	Warn("warning message", "key", "value")
	if len(h.records) == 0 {
		t.Error("Warn: no record captured")
	}
	rec := h.records[0]
	if rec.Level != slog.LevelWarn {
		t.Errorf("Warn: level = %v, want %v", rec.Level, slog.LevelWarn)
	}
	if rec.Message != "warning message" {
		t.Errorf("Warn: message = %q, want %q", rec.Message, "warning message")
	}
}

func TestError(t *testing.T) {
	Init(true)
	h := newCaptureHandler(slog.LevelError)
	SetHandler(h)

	Error("error message", "key", "value")
	if len(h.records) == 0 {
		t.Error("Error: no record captured")
	}
	rec := h.records[0]
	if rec.Level != slog.LevelError {
		t.Errorf("Error: level = %v, want %v", rec.Level, slog.LevelError)
	}
	if rec.Message != "error message" {
		t.Errorf("Error: message = %q, want %q", rec.Message, "error message")
	}
}

func TestScopeOf(t *testing.T) {
	cases := map[string]string{
		"failover: skipping row": "failover",
		"discovery: opened":      "discovery",
		"no colon here":          "",
		":leading colon":         "",
		"ratelimit-ip: throttle": "ratelimit-ip",
	}
	for msg, want := range cases {
		if got := scopeOf(msg); got != want {
			t.Errorf("scopeOf(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestDebug_ScopeFiltering verifies DEBUG_LOG_SCOPES enables Debug for only the
// named scopes while global Debug stays off: matching-scope messages emit,
// others are dropped.
func TestDebug_ScopeFiltering(t *testing.T) {
	t.Setenv("DEBUG_LOG_SCOPES", "failover, discovery")
	Init(false) // global debug off; scopes = {failover, discovery}
	t.Cleanup(func() {
		os.Unsetenv("DEBUG_LOG_SCOPES")
		Init(false)
	})

	// Handler must accept Debug; Init set the level to Debug because scopes are
	// configured, but our capture handler carries its own level.
	if Level() != slog.LevelDebug {
		t.Fatalf("Level() = %v, want Debug when scopes configured", Level())
	}
	h := newCaptureHandler(slog.LevelDebug)
	SetHandler(h)

	Debug("failover: should emit")
	Debug("discovery: should emit")
	Debug("proxy: should be dropped")
	Debug("no-scope message dropped")

	if len(h.records) != 2 {
		t.Fatalf("expected 2 scoped debug records, got %d", len(h.records))
	}
	for _, r := range h.records {
		if s := scopeOf(r.Message); s != "failover" && s != "discovery" {
			t.Errorf("unexpected scoped record emitted: %q (scope %q)", r.Message, s)
		}
	}
}
