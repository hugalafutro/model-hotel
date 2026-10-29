package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// TestWriteError_StatusFollowsTheCause pins the status each error class maps to,
// including the cancelled caller: a browser that hangs up mid-request is not a
// desk failure, so it must stay out of the 5xx range that makes accessLogger
// write an error line.
func TestWriteError_StatusFollowsTheCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"a miss is a 404", ErrNotFound, http.StatusNotFound},
		{"a validation failure is a 400", ErrValidation, http.StatusBadRequest},
		{"a store failure is a 500", errors.New("store closed"), http.StatusInternalServerError},
		{"a cancelled caller is a 499", fmt.Errorf("query row: %w", context.Canceled), httpx.StatusClientClosedRequest},
		{"an expired deadline is still a 500", fmt.Errorf("query row: %w", context.DeadlineExceeded), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tt.err)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

// TestSPAHandlerServesAssetsAndIndex covers spaHandler: a concrete asset is
// served directly, and any unknown route falls back to index.html.
func TestSPAHandlerServesAssetsAndIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>fd</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h := spaHandler(fsys)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("asset request = %d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/spa/route", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>fd</title>") {
		t.Errorf("spa fallback = %d body=%q", rec.Code, rec.Body.String())
	}

	// A chunk from before a redeploy must 404, not come back as index.html.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/gone-deadbeef.js", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", rec.Code)
	}
}
