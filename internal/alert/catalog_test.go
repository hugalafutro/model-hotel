package alert

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// TestCatalogTypesAreEmitted enforces the catalog's documented invariant:
// every alertable Type must correspond to an event actually published
// somewhere in the backend, so an operator never subscribes to a checkbox
// that can never fire.
//
// Unlike Front Desk's fdCatalog (see internal/frontdesk/alerts_test.go's
// TestCatalogTypesAreEmitted, which this is modelled on), the main catalog's
// events are published from packages other than internal/alert itself
// (internal/api, internal/adminauth, internal/failover, cmd/server), so this
// scans the whole backend source tree rather than just the current package.
// catalog.go is excluded so an entry can't "prove" itself by matching its own
// declaration.
//
// circuit_breaker.* is a special case: internal/failover/circuitbreaker.go
// builds that event's Type by string concatenation ("circuit_breaker." +
// state), never as a literal, so a plain quoted-string search would never
// find "circuit_breaker.open" or "circuit_breaker.closed" even though both
// are genuinely emitted. Those are instead verified by confirming the state
// suffix appears as a literal argument to a publishEvent(...) call.
// This is exactly what caught circuit_breaker.half_open as dead: no call
// ever passes "half_open" (nor even "half-open", the state string's actual
// spelling) to publishEvent.
//
// A circuit_breaker.* type that names something other than a circuit state is
// published as a whole literal like every other event, so either form counts.
func TestCatalogTypesAreEmitted(t *testing.T) {
	const repoRoot = "../.."
	skipFile := filepath.Join(repoRoot, "internal", "alert", "catalog.go")

	var src strings.Builder
	for _, dir := range []string{"cmd", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if path == skipFile {
				return nil
			}
			b, rerr := os.ReadFile(path) //nolint:gosec // fixed set of repo-relative paths, not user input
			if rerr != nil {
				return rerr
			}
			src.Write(b)
			src.WriteByte('\n')
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	haystack := src.String()

	const cbPrefix = "circuit_breaker."
	cbCallRe := regexp.MustCompile(`publishEvent\([^)]*"([a-zA-Z-]+)"`)
	cbStates := map[string]bool{}
	for _, m := range cbCallRe.FindAllStringSubmatch(haystack, -1) {
		cbStates[m[1]] = true
	}

	for _, def := range Catalog() {
		// Most circuit_breaker.* types name a circuit STATE and are built by
		// concatenating it, so the literal never appears and the state passed to
		// publishEvent is the only evidence they are emitted. The ones that
		// report something other than a state are published as literals like
		// every other event, so either form counts as wired.
		if strings.HasPrefix(def.Type, cbPrefix) {
			state := strings.TrimPrefix(def.Type, cbPrefix)
			if !cbStates[state] && !strings.Contains(haystack, `"`+def.Type+`"`) {
				t.Errorf("catalog event %q: no publishEvent call passes state %q and the type appears as no literal either; remove the entry or wire the emit", def.Type, state)
			}
			continue
		}
		if !strings.Contains(haystack, `"`+def.Type+`"`) {
			t.Errorf("catalog event %q is never emitted anywhere in the backend; remove it or wire the emit", def.Type)
		}
	}
}
