package frontdesk

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debuglog.Error("frontdesk: encode response", "error", err)
	}
}

// writeError maps store errors to HTTP status codes.
// writeCodedError writes a JSON error body carrying a stable machine-readable
// code alongside the human message, so the frontend can route on the code rather
// than matching translatable English text. Plain-text writeError is kept for the
// many endpoints that need no client-side discrimination.
func writeCodedError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "error": msg})
}

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

// decodeJSON decodes the request body, writing a 400 and returning false on
// failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
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
