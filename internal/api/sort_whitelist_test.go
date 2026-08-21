package api

import (
	"net/url"
	"testing"
)

// hostileSortInputs are values an attacker could send as sort_by; every sort
// surface must map them to a fixed whitelisted column, never into the SQL.
var hostileSortInputs = []string{
	"",
	"name; DROP TABLE models;--",
	"1); DELETE FROM request_logs;--",
	"created_at, (SELECT pg_sleep(10))",
	"time'--",
	"rl.created_at DESC; --",
	"unknown_key",
	"NAME",
	"time ",
}

// TestModelSortColumn_Whitelist pins modelSortColumn to a closed set of
// expressions: any input outside the whitelist must resolve to the default
// name expression. Adding a new sort key requires extending this set, which
// forces a review of the new column expression.
func TestModelSortColumn_Whitelist(t *testing.T) {
	allowed := map[string]bool{
		"COALESCE(m.last_seen_at, m.created_at)": true,
		"COALESCE(m.context_length, 0)":          true,
		"COALESCE(m.max_output_tokens, 0)":       true,
		"COALESCE(p.name, '')":                   true,
		"CASE WHEN m.enabled AND NOT m.disabled_manually THEN 0 WHEN m.enabled AND m.disabled_manually THEN 1 ELSE 2 END": true,
		"COALESCE(m.name, m.model_id, '')": true,
	}
	valid := []string{"name", "discovered", "context", "output", "provider", "status"}
	defaultExpr := modelSortColumn("name")

	for _, in := range append(append([]string{}, valid...), hostileSortInputs...) {
		if got := modelSortColumn(in); !allowed[got] {
			t.Errorf("modelSortColumn(%q) = %q, not in the whitelist", in, got)
		}
	}
	for _, in := range hostileSortInputs {
		if got := modelSortColumn(in); got != defaultExpr {
			t.Errorf("modelSortColumn(%q) = %q, want default %q", in, got, defaultExpr)
		}
	}
}

// TestLogsSortDef_Whitelist pins the request-log sort resolver to its closed
// key set: arbitrary input must normalize to one of the known keys, and
// anything unknown must fall back to "time".
func TestLogsSortDef_Whitelist(t *testing.T) {
	valid := []string{
		"time", "model", "provider", "status", "tokens", "tps", "ttft",
		"response_header_ms", "duration", "overhead", "key",
	}
	validSet := map[string]bool{}
	for _, k := range valid {
		validSet[k] = true
	}

	for _, in := range valid {
		if key, _ := logsSortDef(in); key != in {
			t.Errorf("logsSortDef(%q) normalized to %q, want identity", in, key)
		}
	}
	timeKey, timeDef := logsSortDef("time")
	if timeKey != "time" {
		t.Fatalf("logsSortDef(\"time\") normalized to %q", timeKey)
	}
	for _, in := range hostileSortInputs {
		key, def := logsSortDef(in)
		if !validSet[key] {
			t.Errorf("logsSortDef(%q) normalized to %q, not in the whitelist", in, key)
		}
		if key != "time" || def != timeDef {
			t.Errorf("logsSortDef(%q) = (%q, %+v), want the time fallback", in, key, def)
		}
	}
}

// TestParseAppLogHistoryParams_SortWhitelist pins the app-log history sort to
// its closed column set and the direction to ASC/DESC only.
func TestParseAppLogHistoryParams_SortWhitelist(t *testing.T) {
	allowedCols := map[string]bool{"created_at": true, "level": true, "source": true, "message": true}

	for _, in := range append([]string{"time", "level", "source", "message"}, hostileSortInputs...) {
		for _, dir := range []string{"asc", "desc", "asc; DROP TABLE app_logs;--", ""} {
			p := parseAppLogHistoryParams(url.Values{"sort_by": {in}, "sort_dir": {dir}})
			if !allowedCols[p.sortCol] {
				t.Errorf("sort_by=%q resolved to column %q, not in the whitelist", in, p.sortCol)
			}
			if p.sortDir != "ASC" && p.sortDir != "DESC" {
				t.Errorf("sort_dir=%q resolved to %q, want ASC or DESC", dir, p.sortDir)
			}
		}
	}
}
