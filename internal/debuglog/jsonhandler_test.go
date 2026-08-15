package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLevelName(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug:     "debug",
		slog.LevelInfo:      "info",
		slog.LevelWarn:      "warning",
		slog.LevelError:     "error",
		slog.LevelError + 4: "error",
		slog.LevelDebug - 4: "debug",
	}
	for l, want := range cases {
		if got := LevelName(l); got != want {
			t.Errorf("LevelName(%v) = %q, want %q", l, got, want)
		}
	}
}

func TestSplitSource(t *testing.T) {
	cases := []struct{ in, source, msg string }{
		{"proxy: request failed", "proxy", "request failed"},
		{"[proxy] request failed", "proxy", "request failed"},
		{"admin-chat: hi", "admin-chat", "hi"},
		{"db.pool: hi", "db.pool", "hi"},
		{"no prefix here", "", "no prefix here"},
		{"x: too short a source", "", "x: too short a source"},
		{"1abc: digit first", "", "1abc: digit first"},
		{"has space: nope", "", "has space: nope"},
		{"[unterminated message", "", "[unterminated message"},
		{"", "", ""},
	}
	for _, c := range cases {
		source, msg := SplitSource(c.in)
		if source != c.source || msg != c.msg {
			t.Errorf("SplitSource(%q) = (%q, %q), want (%q, %q)", c.in, source, msg, c.source, c.msg)
		}
	}
}

func TestJSONLine_ReservedKeysWin(t *testing.T) {
	ts := time.Date(2026, 8, 15, 10, 0, 0, 123, time.FixedZone("x", 3600))
	line := JSONLine(ts, "warning", "proxy", "hello", map[string]string{"level": "bogus", "attempt": "2"})
	var got map[string]string
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, line)
	}
	want := map[string]string{
		"time": "2026-08-15T09:00:00.000000123Z", "level": "warning", "source": "proxy", "msg": "hello", "attempt": "2",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected keys: %v", got)
	}
}

// The stdout JSON handler must produce the documented LOG_FORMAT=json shape -
// lowercase level, source split out of the message, attrs as fields - so a
// collector sees one record shape whether a line came from this handler or
// from the dashboard's app-log handler.
func TestJSONHandler_Shape(t *testing.T) {
	var buf bytes.Buffer
	h := newJSONHandler(&buf, slog.LevelInfo)
	logger := slog.New(h)

	logger.Warn("db: migration failed", "name", "007.sql", "error", errors.New("boom"), "took", 1500*time.Millisecond)
	logger.Debug("db: dropped, below level")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 line, got %d: %q", len(lines), buf.String())
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, lines[0])
	}
	want := map[string]string{"level": "warning", "source": "db", "msg": "migration failed", "name": "007.sql", "error": "boom", "took": "1.5s"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, got["time"]); err != nil {
		t.Errorf("time %q is not RFC3339Nano: %v", got["time"], err)
	}
}

func TestJSONHandler_WithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	base := newJSONHandler(&buf, slog.LevelDebug)
	if base.WithAttrs(nil) != base || base.WithGroup("") != base {
		t.Fatal("empty WithAttrs/WithGroup must return the receiver")
	}
	h := base.WithAttrs([]slog.Attr{slog.String("request_id", "r1")}).WithGroup("http").WithGroup("client")
	grouped := h.WithAttrs([]slog.Attr{slog.String("ua", "curl")})
	if err := grouped.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "access: request", 0)); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(grouped)
	logger.Info("access: request", "status", 200)

	var got map[string]string
	last := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if err := json.Unmarshal([]byte(last[len(last)-1]), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	// slog semantics: an attr is qualified by the groups open when it was
	// added. request_id came before any group, so it stays top-level; the
	// record's attrs are namespaced by both groups.
	if got["request_id"] != "r1" {
		t.Errorf("request_id = %q, want r1 (attrs added before the group are not namespaced)", got["request_id"])
	}
	if got["http.client.status"] != "200" {
		t.Errorf("http.client.status = %q, want 200; line=%v", got["http.client.status"], got)
	}
	if got["http.client.ua"] != "curl" {
		t.Errorf("http.client.ua = %q, want curl (attr added inside the group is namespaced)", got["http.client.ua"])
	}
	// Base handler is untouched by the derived ones.
	if len(base.attrs) != 0 || base.group != "" {
		t.Error("derived handlers must not mutate the base")
	}
}
