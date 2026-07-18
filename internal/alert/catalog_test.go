package alert

import (
	"strings"
	"testing"
)

func TestCatalogReturnsCopy(t *testing.T) {
	a := Catalog()
	if len(a) == 0 {
		t.Fatal("catalog is empty")
	}
	a[0].Type = "mutated"
	if Catalog()[0].Type == "mutated" {
		t.Error("Catalog() leaked a reference to the underlying registry")
	}
}

func TestCatalogEntriesAreWellFormed(t *testing.T) {
	validSeverity := map[string]bool{"success": true, "info": true, "warning": true, "error": true}
	seen := map[string]bool{}
	for _, e := range Catalog() {
		if e.Type == "" || e.Category == "" {
			t.Errorf("catalog entry missing Type/Category: %+v", e)
		}
		if !validSeverity[e.Severity] {
			t.Errorf("catalog entry %q has invalid severity %q", e.Type, e.Severity)
		}
		if seen[e.Type] {
			t.Errorf("duplicate catalog Type %q", e.Type)
		}
		seen[e.Type] = true
	}
}

func TestDefaultEnabledCSV(t *testing.T) {
	csv := DefaultEnabledCSV()
	got := ParseEnabled(csv)
	for _, e := range Catalog() {
		if e.DefaultOn && !got[e.Type] {
			t.Errorf("default-on event %q missing from DefaultEnabledCSV", e.Type)
		}
		if !e.DefaultOn && got[e.Type] {
			t.Errorf("default-off event %q unexpectedly in DefaultEnabledCSV", e.Type)
		}
	}
}

func TestParseEnabled(t *testing.T) {
	got := ParseEnabled(" a , ,b,c , ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("ParseEnabled returned %d entries, want %d (%v)", len(got), len(want), got)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("ParseEnabled missing %q", w)
		}
	}
	if len(ParseEnabled("")) != 0 {
		t.Error("ParseEnabled(\"\") should be empty")
	}
}

func TestDefaultEnabledCSVOnlyKnownTypes(t *testing.T) {
	idx := catalogIndex()
	for tpe := range strings.SplitSeq(DefaultEnabledCSV(), ",") {
		if _, ok := idx[tpe]; !ok {
			t.Errorf("DefaultEnabledCSV contains unknown type %q", tpe)
		}
	}
}
