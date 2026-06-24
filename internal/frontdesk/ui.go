package frontdesk

import (
	"embed"
	"io/fs"
)

// The Front Desk SPA is authored in frontdesk/web/ (its own pnpm package, kept
// out of the main web/ workspace on purpose) and built into this webui/
// directory so it can be embedded into the binary. The committed tree carries
// only webui/.gitkeep; Dockerfile.frontdesk (and `make frontdesk-build`) copy
// the Vite output of frontdesk/web/dist into webui/ before `go build`, so a
// production binary serves the real UI while a bare `go build` (tests, CI Go
// jobs) compiles fine against the placeholder and runs API-only.
//
//go:embed all:webui
var webuiFS embed.FS

// EmbeddedUI returns the built Front Desk SPA rooted at its asset directory, or
// nil when no build is embedded (only the placeholder is present). The server
// mounts the SPA only when this is non-nil, so an API-only binary is a valid,
// non-fatal configuration rather than a startup error.
func EmbeddedUI() fs.FS {
	sub, err := fs.Sub(webuiFS, "webui")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
