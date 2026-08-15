package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
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
		{"[] something", "", "something"}, // empty brackets: no source, prefix still stripped
		{"[proxy]no space", "", "[proxy]no space"},
		{"[proxy] access: request", "proxy", "access: request"},
		{"circuit-breaker: provider state=open", "circuit-breaker", "provider state=open"},
		{"models.dev: loaded models", "models.dev", "loaded models"},
		{"TRUSTED_PROXIES: skipping invalid CIDR", "TRUSTED_PROXIES", "skipping invalid CIDR"},
		{"hello@world: message", "", "hello@world: message"},
		{"", "", ""},
	}
	for _, c := range cases {
		source, msg := SplitSource(c.in)
		if source != c.source || msg != c.msg {
			t.Errorf("SplitSource(%q) = (%q, %q), want (%q, %q)", c.in, source, msg, c.source, c.msg)
		}
	}
}

func TestJSONLine_ReservedKeysWinAndZeroTimeOmitted(t *testing.T) {
	ts := time.Date(2026, 8, 15, 10, 0, 0, 123, time.FixedZone("x", 3600))
	line := JSONLine(ts, "warning", "proxy", "hello", map[string]any{"level": "bogus", "attempt": int64(2)})
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, line)
	}
	want := map[string]any{
		"time": "2026-08-15T09:00:00.000000123Z", "level": "warning", "source": "proxy", "msg": "hello", "attempt": float64(2),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v, want %#v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected keys: %v", got)
	}

	// slog contract: a zero time is not rendered.
	got = nil
	if err := json.Unmarshal(JSONLine(time.Time{}, "info", "", "x", nil), &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["time"]; present {
		t.Errorf("zero time must be omitted, got %v", got["time"])
	}
}

type redacted struct{ secret string }

func (redacted) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// masked mimics config.Config: a Stringer that hides fields reflection would dump.
type masked struct{ Secret string }

func (masked) String() string { return "***" }

type customJSON struct{ Secret string }

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`{"custom":true}`), nil }

type nilableErr struct{}

func (*nilableErr) Error() string { return "never called" }

type marshalable struct {
	A int    `json:"a"`
	B string `json:"b"`
}

// AddJSONField is the value contract shared by every JSON emitter: JSON types
// preserved where they exist, textual forms elsewhere, LogValuers resolved,
// zero attrs skipped, groups expanded into dotted keys.
func TestAddJSONField_ValueRules(t *testing.T) {
	fields := map[string]any{}
	for _, a := range []slog.Attr{
		slog.String("s", "x"),
		slog.Int("i", 3),
		slog.Uint64("u", 4),
		slog.Float64("f", 1.5),
		slog.Bool("b", true),
		slog.Duration("d", 1500*time.Millisecond),
		slog.Time("t", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		slog.Any("err", errors.New("boom")),
		slog.Any("obj", marshalable{A: 1, B: "two"}),
		slog.Any("nilv", nil),
		slog.Any("lv", redacted{secret: "hunter2"}),
		slog.Any("unmarshalable", make(chan int)),
		slog.Any("stringer", masked{Secret: "hunter2"}),
		slog.Any("marshaler", customJSON{Secret: "hunter2"}),
		slog.Any("typed_nil_err", (*nilableErr)(nil)),
		slog.Float64("nan", math.NaN()),
		slog.Float64("inf", math.Inf(1)),
		slog.String("", "keyless"), // empty key: dropped
		{},                         // zero Attr: ignored
		slog.Group("g", slog.Int("n", 1), slog.Group("inner", slog.Bool("ok", true))),
		slog.Group("", slog.String("inlined", "yes")),
		slog.Group("empty"),
	} {
		AddJSONField(fields, "", a)
	}
	line, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("fields must marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"s": "x", "i": float64(3), "u": float64(4), "f": 1.5, "b": true,
		"d": "1.5s", "t": "2026-01-02T03:04:05Z", "err": "boom",
		"nilv": nil, "lv": "[redacted]",
		"g.n": float64(1), "g.inner.ok": true, "inlined": "yes",
		"stringer": "***", "typed_nil_err": nil, "nan": "NaN", "inf": "+Inf",
	}
	for k, v := range want {
		gv, present := got[k]
		if !present || gv != v {
			t.Errorf("%s = %#v (present=%v), want %#v", k, gv, present, v)
		}
	}
	if obj, ok := got["obj"].(map[string]any); !ok || obj["a"] != float64(1) || obj["b"] != "two" {
		t.Errorf("obj = %#v, want a JSON object {a:1,b:two}", got["obj"])
	}
	if s, ok := got["unmarshalable"].(string); !ok || s == "" {
		t.Errorf("unmarshalable value must fall back to its textual form, got %#v", got["unmarshalable"])
	}
	if strings.Contains(string(line), "hunter2") {
		t.Errorf("a masking Stringer/LogValuer/Marshaler must win over reflection: %s", line)
	}
	if cj, ok := got["marshaler"].(map[string]any); !ok || cj["custom"] != true {
		t.Errorf("marshaler = %#v, want its own MarshalJSON form", got["marshaler"])
	}
	for _, absent := range []string{"", "empty", "g"} {
		if _, present := got[absent]; present {
			t.Errorf("key %q must not be present (zero attr / empty group / group container)", absent)
		}
	}
}

// The stdout JSON handler must produce the documented LOG_FORMAT=json shape -
// lowercase level, source split out of the message, attrs as typed fields - so
// a collector sees one record shape whether a line came from this handler or
// from the dashboard's app-log handler.
func TestJSONHandler_Shape(t *testing.T) {
	var buf bytes.Buffer
	h := newJSONHandler(&buf, slog.LevelInfo)
	logger := slog.New(h)

	logger.Warn("db: migration failed", "name", "007.sql", "error", errors.New("boom"), "took", 1500*time.Millisecond, "attempt", 2, "fatal", false)
	logger.Debug("db: dropped, below level")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 line, got %d: %q", len(lines), buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, lines[0])
	}
	want := map[string]any{"level": "warning", "source": "db", "msg": "migration failed", "name": "007.sql", "error": "boom", "took": "1.5s", "attempt": float64(2), "fatal": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v, want %#v", k, got[k], v)
		}
	}
	if ts, _ := got["time"].(string); ts == "" {
		t.Errorf("time missing: %v", got)
	} else if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("time %q is not RFC3339Nano: %v", ts, err)
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

	var got map[string]any
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
	if got["http.client.status"] != float64(200) {
		t.Errorf("http.client.status = %#v, want 200; line=%v", got["http.client.status"], got)
	}
	if got["http.client.ua"] != "curl" {
		t.Errorf("http.client.ua = %q, want curl (attr added inside the group is namespaced)", got["http.client.ua"])
	}
	// Base handler is untouched by the derived ones.
	if len(base.attrs) != 0 || base.group != "" {
		t.Error("derived handlers must not mutate the base")
	}
}
