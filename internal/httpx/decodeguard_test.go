package httpx

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unboundedDecode is the call that must not appear outside this package: it
// reads the request body with no ceiling, so a single request can make the
// server buffer as much as the client is willing to send.
const unboundedDecode = "json.NewDecoder(r.Body)"

// skipDir is the directory names the guard does not descend into: source
// control and the frontends' dependency trees hold no Go of ours and would
// dominate the walk.
var skipDir = map[string]bool{".git": true, "node_modules": true}

// TestNoUnboundedJSONDecode keeps the bounded-decode contract from decaying.
// DecodeJSON exists so no HTTP surface reads an unbounded request body, but a
// helper only holds while handlers actually use it, and the tempting one-liner
// is exactly what a new handler copies from its neighbours. The guard lives
// beside the contract rather than in each consumer package so there is one copy
// of it, and it walks the whole repository rather than a list of packages so a
// new surface is covered the day it is added instead of the day someone
// remembers to register it. The repository and not just internal/, because a
// handler defined in cmd/ or a tool would be exactly as unbounded.
//
// Only this package is exempt, because DecodeJSON is where the bounded read is
// implemented.
//
// What it does not catch, stated so nobody mistakes it for more than it is: it
// matches one call shape, so io.ReadAll(r.Body) followed by json.Unmarshal (the
// idiom internal/proxy uses for the deliberately larger /v1 data plane) passes,
// and it says nothing about whether a limit is a sensible size. It is a
// backstop against the one-line copy, not a proof of boundedness.
func TestNoUnboundedJSONDecode(t *testing.T) {
	selfDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package dir: %v", err)
	}

	// The package sits at internal/httpx, so the repository root is two up.
	walkErr := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory the test process cannot read (the dev stack's
			// bind-mounted pgdata is root-owned) holds none of our source, so
			// skipping it keeps the guard usable on a working checkout.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if filepath.Dir(abs) == selfDir {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, unboundedDecode) {
				continue
			}
			// A comment may legitimately name the call (this file's own const
			// does), and a line that binds the reader in place is bounded even
			// though it matches.
			if strings.HasPrefix(strings.TrimSpace(line), "//") ||
				strings.Contains(line, "MaxBytesReader") || strings.Contains(line, "LimitReader") {
				continue
			}
			t.Errorf("%s:%d reads the request body without a size limit; decode through the package's decodeJSON helper (httpx.DecodeJSON) instead", path, i+1)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk the repository: %v", walkErr)
	}
}
