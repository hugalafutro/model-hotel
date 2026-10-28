package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/logtest"
)

// The helper bodies live in internal/httpx and are tested there. What this
// package owns is the wrapper layer: it must keep the "api" log prefix and the
// argument order the ~250 call sites rely on.

// lastMsg is the message of the most recent record capt saw, "" when none.
func lastMsg(capt *logtest.Handler) string {
	r, _ := capt.Latest()
	return r.Msg
}

func TestRespondError_UsesAPIPrefix(t *testing.T) {
	capt := logtest.Capture(t)
	w := httptest.NewRecorder()
	respondError(w, "failed to load thing", errors.New("db down"), http.StatusInternalServerError)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if got := lastMsg(capt); got != "api: failed to load thing" {
		t.Errorf("log message = %q, want the api prefix", got)
	}
}

func TestRespondBadRequest_UsesAPIPrefix(t *testing.T) {
	capt := logtest.Capture(t)
	w := httptest.NewRecorder()
	respondBadRequest(w, "invalid JSON body", errors.New("unexpected EOF"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if got := lastMsg(capt); got != "api: bad request: invalid JSON body" {
		t.Errorf("log message = %q, want the api prefix", got)
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
		capt := logtest.Capture(t)
		w := httptest.NewRecorder()
		respondLookupError(w, errors.New("db connection lost"), pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if got := lastMsg(capt); got != "api: failed to load thing" {
			t.Errorf("log message = %q, want the api prefix", got)
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
