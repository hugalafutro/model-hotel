package frontdesk

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// logComponent is the log prefix the shared response helpers write under.
const logComponent = "frontdesk"

// writeJSON encodes v as JSON with an explicit status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSONStatus(w, logComponent, status, v)
}

// writeCodedError writes a JSON error body carrying a stable machine-readable
// code alongside the human message, so the frontend can route on the code rather
// than matching translatable English text. Plain-text writeError is kept for the
// many endpoints that need no client-side discrimination.
func writeCodedError(w http.ResponseWriter, status int, code, msg string) {
	httpx.WriteCodedError(w, logComponent, status, code, msg)
}

// writeError maps store errors to HTTP status codes.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrValidation), errors.Is(err, ErrDuplicateURL), errors.Is(err, ErrInsecureURL):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		debuglog.Error("frontdesk: request failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// decodeJSON bounds the request body to httpx.MaxJSONBody and decodes it,
// writing the 400/413 response itself and reporting whether the handler may
// continue. The bound matters most for the pairing exchange, which is
// unauthenticated by design (only the per-IP limiter stands in front of it), so
// without a ceiling any caller that can reach Front Desk could make it read an
// arbitrarily large body. Every handler in this package decodes through here;
// internal/httpx's TestNoUnboundedJSONDecode guard keeps it that way.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return httpx.DecodeJSON(w, r, logComponent, httpx.MaxJSONBody, v)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// Event listing page-size bounds. A request with no/blank limit gets the
// default; a non-positive limit would otherwise disable the store's LIMIT clause
// (unbounded query), and an over-large one could return the whole table, so both
// ends are clamped here.
const (
	defaultEventsLimit = 100
	maxEventsLimit     = 500
)

// clampEventsLimit forces an events page size into [1, maxEventsLimit].
func clampEventsLimit(n int) int {
	if n < 1 {
		return defaultEventsLimit
	}
	if n > maxEventsLimit {
		return maxEventsLimit
	}
	return n
}

// parseRFC3339 parses an RFC3339 timestamp from a query value, returning the
// zero time (which EventFilter treats as "no bound") when empty or malformed.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// spaHandler serves the embedded single-page app, falling back to index.html for
// client-side routes (any path without a file extension that is not found).
func spaHandler(ui fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fs.ValidPath + the embedded FS are the traversal boundary: "../" or an
		// absolute name is rejected here and falls through to the SPA index, and
		// http.FileServer additionally cleans the path it serves. Only serve a
		// concrete asset when it exists and the name is valid.
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && fs.ValidPath(name) {
			if _, err := fs.Stat(ui, name); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Root, invalid, or unknown path: serve index.html so the SPA router can
		// handle the route client-side.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
