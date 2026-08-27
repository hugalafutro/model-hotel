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

// TestNoUnboundedJSONDecode keeps the bounded-decode contract from decaying.
// DecodeJSON exists so no HTTP surface reads an unbounded request body, but a
// helper only holds while handlers actually use it, and the tempting one-liner
// is exactly what a new handler copies from its neighbours. The guard lives
// beside the contract rather than in each consumer package so there is one copy
// of it, and it walks all of internal/ rather than a list of packages so a new
// surface is covered the day it is added instead of the day someone remembers
// to register it.
//
// Only this package is exempt, because DecodeJSON is where the bounded read is
// implemented.
func TestNoUnboundedJSONDecode(t *testing.T) {
	selfDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package dir: %v", err)
	}

	walkErr := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if filepath.Dir(abs) == selfDir {
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // walking this repo's own source tree
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, unboundedDecode) {
				t.Errorf("%s:%d reads the request body without a size limit; decode through the package's decodeJSON helper (httpx.DecodeJSON) instead", path, i+1)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk internal/: %v", walkErr)
	}
}
