package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The helper bodies live in internal/httpx and are tested there. What this
// package owns is the wrapper layer: it must keep the "api" log prefix and the
// argument order the ~250 call sites rely on.

// msgCaptureHandler records the message of the last record it handled.
type msgCaptureHandler struct{ msg string }

func (h *msgCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *msgCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.msg = r.Message
	return nil
}
func (h *msgCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *msgCaptureHandler) WithGroup(string) slog.Handler      { return h }

func captureAPILogs(t *testing.T) *msgCaptureHandler {
	t.Helper()
	// SetHandler swaps the process-wide slog default; restore it afterwards so
	// later tests in this package aren't silently swallowed by the capture handler.
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	capt := &msgCaptureHandler{}
	debuglog.SetHandler(capt)
	return capt
}

func TestRespondError_UsesAPIPrefix(t *testing.T) {
	capt := captureAPILogs(t)
	w := httptest.NewRecorder()
	respondError(w, "failed to load thing", errors.New("db down"), http.StatusInternalServerError)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if capt.msg != "api: failed to load thing" {
		t.Errorf("log message = %q, want the api prefix", capt.msg)
	}
}

func TestRespondBadRequest_UsesAPIPrefix(t *testing.T) {
	capt := captureAPILogs(t)
	w := httptest.NewRecorder()
	respondBadRequest(w, "invalid JSON body", errors.New("unexpected EOF"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if capt.msg != "api: bad request: invalid JSON body" {
		t.Errorf("log message = %q, want the api prefix", capt.msg)
	}
}

func TestRespondLookupError(t *testing.T) {
	t.Run("not-found sentinel returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondLookupError(w, pgx.ErrNoRows, pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("any other error returns a logged 500 under the api prefix", func(t *testing.T) {
		capt := captureAPILogs(t)
		w := httptest.NewRecorder()
		respondLookupError(w, errors.New("db connection lost"), pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if capt.msg != "api: failed to load thing" {
			t.Errorf("log message = %q, want the api prefix", capt.msg)
		}
	})
}

func TestWriteJSONHelpers(t *testing.T) {
	t.Run("writeJSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeJSON(w, map[string]string{"a": "b"})
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("got %d %q", w.Code, w.Header().Get("Content-Type"))
		}
	})

	t.Run("writeJSONCreated writes 201", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeJSONCreated(w, map[string]string{"a": "b"})
		if w.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", w.Code)
		}
	})

	t.Run("writeJSONStatus writes the given status", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"a": "b"})
		if w.Code != http.StatusAccepted {
			t.Errorf("status = %d, want 202", w.Code)
		}
	})

	t.Run("writeCodedError writes {code,error}", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeCodedError(w, http.StatusConflict, "duplicate", "already exists")
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
		if got := w.Body.String(); got != "{\"code\":\"duplicate\",\"error\":\"already exists\"}\n" {
			t.Errorf("body = %q", got)
		}
	})
}
