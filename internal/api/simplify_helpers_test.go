package api

import (
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCursorPageParams_LimitClampsRatherThanFallsBack pins the one behaviour
// the three cursor endpoints used to disagree on: an out-of-range limit is
// clamped to the ceiling, not silently reset to the default.
func TestCursorPageParams_LimitClampsRatherThanFallsBack(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 20},
		{"abc", 20},
		{"50", 50},
		{"0", 1},
		{"-5", 1},
		{"500", 200},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		limit, _, _, _, ok := cursorPageParams(w, url.Values{"limit": {tc.raw}}, 20, 200)
		if !ok {
			t.Fatalf("limit=%q: unexpected rejection", tc.raw)
		}
		if limit != tc.want {
			t.Errorf("limit=%q: got %d, want %d", tc.raw, limit, tc.want)
		}
	}
}

func TestCursorPageParams_DirectionAndCursor(t *testing.T) {
	c := logCursor{CreatedAt: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC), ID: uuid.NewString()}
	w := httptest.NewRecorder()
	_, cursorStr, direction, decoded, ok := cursorPageParams(w,
		url.Values{"direction": {"before"}, "cursor": {c.encode()}}, 20, 200)
	if !ok {
		t.Fatal("valid cursor rejected")
	}
	if direction != "before" || cursorStr == "" {
		t.Fatalf("direction=%q cursorStr=%q", direction, cursorStr)
	}
	if !decoded.CreatedAt.Equal(c.CreatedAt) || decoded.ID != c.ID {
		t.Errorf("decoded = %+v, want %+v", decoded, c)
	}

	// Anything that is not "before" means "after".
	if _, _, dir, _, _ := cursorPageParams(httptest.NewRecorder(), url.Values{"direction": {"sideways"}}, 20, 200); dir != "after" {
		t.Errorf("direction = %q, want after", dir)
	}

	// An undecodable cursor is a written 400, not a silent default.
	w = httptest.NewRecorder()
	if _, _, _, _, ok := cursorPageParams(w, url.Values{"cursor": {"!!not-base64!!"}}, 20, 200); ok {
		t.Error("expected the bad cursor to be rejected")
	}
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestNamesFor_PreservesNullness covers the distinction the import reads as
// "restricted" vs "unrestricted": nil stays nil, and a list whose ids have all
// been deleted stays present but empty.
func TestNamesFor_PreservesNullness(t *testing.T) {
	lookup := map[string]string{"id-a": "alpha", "id-b": "beta"}

	if got := namesFor(nil, lookup); got != nil {
		t.Errorf("nil ids should stay nil, got %v", *got)
	}
	got := namesFor([]string{"id-gone"}, lookup)
	if got == nil {
		t.Fatal("a present list must stay present even when nothing resolves")
	}
	if len(*got) != 0 {
		t.Errorf("names = %v, want empty", *got)
	}
	got = namesFor([]string{"id-b", "id-gone", "id-a"}, lookup)
	if !slices.Equal(*got, []string{"beta", "alpha"}) {
		t.Errorf("names = %v, want [beta alpha]", *got)
	}
}

func TestTranslateIDs_DropsUnresolvable(t *testing.T) {
	got := translateIDs([]string{"a", "x", "b"}, map[string]string{"a": "1", "b": "2"})
	if !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("translateIDs = %v, want [1 2]", got)
	}
	if got := translateIDs(nil, map[string]string{"a": "1"}); len(got) != 0 {
		t.Errorf("translateIDs(nil) = %v, want empty", got)
	}
}

func TestWithinRelTolerance(t *testing.T) {
	cases := []struct {
		o, n, tol float64
		want      bool
	}{
		{0, 0, 0.01, true}, // both zero: no relative difference to measure
		{100, 100.5, 0.01, true},
		{100, 120, 0.01, false},
		{0, 5, 0.01, false}, // a fill is a real change
		{-100, -100.5, 0.01, true},
	}
	for _, tc := range cases {
		if got := withinRelTolerance(tc.o, tc.n, tc.tol); got != tc.want {
			t.Errorf("withinRelTolerance(%v, %v, %v) = %v, want %v", tc.o, tc.n, tc.tol, got, tc.want)
		}
	}
}

// TestPgCommandEnv_MovesPasswordOutOfTheURL covers what both the dump and the
// restore rely on: the password never reaches the command line.
func TestPgCommandEnv_MovesPasswordOutOfTheURL(t *testing.T) {
	connURL, env := pgCommandEnv("postgres://bob:s3cret@db:5432/hotel")
	if connURL != "postgres://bob@db:5432/hotel" {
		t.Errorf("connURL = %q, still carries the password?", connURL)
	}
	if !slices.Contains(env, "PGPASSWORD=s3cret") {
		t.Error("PGPASSWORD not exported")
	}
	if len(env) <= len(os.Environ()) {
		t.Error("env should extend the parent environment, not replace it")
	}

	// No password: the command stays on the inherited environment.
	connURL, env = pgCommandEnv("postgres://bob@db:5432/hotel")
	if connURL != "postgres://bob@db:5432/hotel" || env != nil {
		t.Errorf("passwordless URL: connURL=%q env=%v", connURL, env)
	}
	if connURL, env := pgCommandEnv("::not a url::"); connURL != "::not a url::" || env != nil {
		t.Errorf("unparseable URL: connURL=%q env=%v", connURL, env)
	}
}

func TestTimeSeriesBucket_GranularityPerPeriod(t *testing.T) {
	cases := []struct {
		period time.Duration
		step   time.Duration
		expr   string
	}{
		{time.Hour, 5 * time.Minute, "date_bin('5 minutes', rl.created_at, '2000-01-01')"},
		{24 * time.Hour, time.Hour, "date_trunc('hour', rl.created_at)"},
		{7 * 24 * time.Hour, 24 * time.Hour, "date_trunc('day', rl.created_at)"},
		{30 * 24 * time.Hour, 24 * time.Hour, "date_trunc('day', rl.created_at)"},
	}
	for _, tc := range cases {
		spec := timeSeriesBucket(tc.period)
		if spec.step != tc.step || spec.expr != tc.expr {
			t.Errorf("period %v: got step=%v expr=%q", tc.period, spec.step, spec.expr)
		}
		if spec.window < spec.step*time.Duration(spec.expected-1) {
			t.Errorf("period %v: window %v cannot hold %d buckets of %v", tc.period, spec.window, spec.expected, spec.step)
		}
	}
}

func TestValidateAllowedProvidersShape(t *testing.T) {
	if !validateAllowedProvidersShape(httptest.NewRecorder(), nil) {
		t.Error("nil (no restriction) must be accepted")
	}
	list := []string{"p1"}
	if !validateAllowedProvidersShape(httptest.NewRecorder(), &list) {
		t.Error("a populated list must be accepted")
	}
	w := httptest.NewRecorder()
	empty := []string{}
	if validateAllowedProvidersShape(w, &empty) {
		t.Error("a present-but-empty list must be refused")
	}
	if w.Code != 400 || w.Body.String() != errEmptyAllowedProviders.Error()+"\n" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestValidateVirtualKeyName(t *testing.T) {
	if got, ok := validateVirtualKeyName(httptest.NewRecorder(), "  my key  "); !ok || got != "my key" {
		t.Errorf("validateVirtualKeyName = %q, %v", got, ok)
	}
	for _, reserved := range []string{"chat", "ARENA", "Completions", "admin"} {
		w := httptest.NewRecorder()
		if _, ok := validateVirtualKeyName(w, reserved); ok {
			t.Errorf("%q must be refused as reserved", reserved)
		}
		if w.Code != 400 {
			t.Errorf("%q: status = %d, want 400", reserved, w.Code)
		}
	}
	if _, ok := validateVirtualKeyName(httptest.NewRecorder(), ""); ok {
		t.Error("an empty name must be refused")
	}
}
