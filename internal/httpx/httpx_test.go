package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// levelCaptureHandler records the level and message of the last record it handled.
type levelCaptureHandler struct {
	last slog.Level
	msg  string
}

func (h *levelCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.last = r.Level
	h.msg = r.Message
	return nil
}
func (h *levelCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *levelCaptureHandler) WithGroup(string) slog.Handler      { return h }

// captureLogs swaps the process-wide slog default for a recorder and restores
// it afterwards so later tests are not swallowed by the capture handler.
func captureLogs(t *testing.T) *levelCaptureHandler {
	t.Helper()
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	capt := &levelCaptureHandler{}
	debuglog.SetHandler(capt)
	return capt
}

// errWriter fails every write, so the JSON encoder inside the write helpers
// reports the failure the logging branches exist for.
type errWriter struct {
	hdr http.Header
	err error
	// code records the status the helper wrote before the body failed.
	code int
}

func newErrWriter(err error) *errWriter {
	return &errWriter{hdr: http.Header{}, err: err}
}

func (w *errWriter) Header() http.Header       { return w.hdr }
func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }
func (w *errWriter) WriteHeader(code int)      { w.code = code }

func TestRespondError(t *testing.T) {
	t.Run("logs the error and writes the message", func(t *testing.T) {
		capt := captureLogs(t)
		w := httptest.NewRecorder()
		RespondError(w, "api", "failed to load thing", errors.New("db down"), http.StatusInternalServerError)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
		if got := strings.TrimSpace(w.Body.String()); got != "failed to load thing" {
			t.Errorf("body = %q, want the sanitized message", got)
		}
		if capt.msg != "api: failed to load thing" {
			t.Errorf("log message = %q, want the component prefix", capt.msg)
		}
		if capt.last != slog.LevelError {
			t.Errorf("log level = %v, want error", capt.last)
		}
	})

	t.Run("5xx without an error value is still logged", func(t *testing.T) {
		capt := captureLogs(t)
		w := httptest.NewRecorder()
		RespondError(w, "adminauth", "upstream unavailable", nil, http.StatusBadGateway)
		if capt.msg != "adminauth: upstream unavailable" {
			t.Errorf("log message = %q, want the message logged for a 5xx", capt.msg)
		}
		if capt.last != slog.LevelError {
			t.Errorf("log level = %v, want error", capt.last)
		}
	})

	t.Run("4xx without an error value is not logged", func(t *testing.T) {
		capt := captureLogs(t)
		capt.msg = "sentinel"
		w := httptest.NewRecorder()
		RespondError(w, "api", "nope", nil, http.StatusForbidden)
		if capt.msg != "sentinel" {
			t.Errorf("log message = %q, want no log for a client-facing 4xx", capt.msg)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})
}

func TestRespondLookupError(t *testing.T) {
	notFound := errors.New("no rows")

	t.Run("not-found sentinel returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		RespondLookupError(w, "api", notFound, notFound, "thing not found", "failed to load thing")
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
		if got := strings.TrimSpace(w.Body.String()); got != "thing not found" {
			t.Errorf("body = %q, want the not-found message", got)
		}
	})

	t.Run("wrapped not-found sentinel returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		wrapped := fmt.Errorf("query failed: %w", notFound)
		RespondLookupError(w, "api", wrapped, notFound, "thing not found", "failed to load thing")
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for wrapped sentinel, got %d", w.Code)
		}
	})

	t.Run("any other error returns a logged 500", func(t *testing.T) {
		capt := captureLogs(t)
		w := httptest.NewRecorder()
		RespondLookupError(w, "api", errors.New("db connection lost"), notFound, "thing not found", "failed to load thing")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if capt.msg != "api: failed to load thing" {
			t.Errorf("log message = %q, want the load message", capt.msg)
		}
	})
}

func TestRespondBadRequest(t *testing.T) {
	t.Run("logs the cause at info", func(t *testing.T) {
		capt := captureLogs(t)
		w := httptest.NewRecorder()
		RespondBadRequest(w, "api", "invalid JSON body", errors.New("unexpected EOF"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
		if capt.msg != "api: bad request: invalid JSON body" {
			t.Errorf("log message = %q", capt.msg)
		}
		if capt.last != slog.LevelInfo {
			t.Errorf("log level = %v, want info", capt.last)
		}
	})

	t.Run("nil error logs nothing", func(t *testing.T) {
		capt := captureLogs(t)
		capt.msg = "sentinel"
		w := httptest.NewRecorder()
		RespondBadRequest(w, "api", "missing field", nil)
		if capt.msg != "sentinel" {
			t.Errorf("log message = %q, want no log without a cause", capt.msg)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

func TestParseUUIDParam(t *testing.T) {
	// serve routes a request through chi so URLParam resolves, returning what
	// ParseUUIDParam produced plus the recorder.
	serve := func(pattern, path string, label ...string) (uuid.UUID, bool, *httptest.ResponseRecorder) {
		var (
			gotID uuid.UUID
			gotOK bool
		)
		r := chi.NewRouter()
		r.Get(pattern, func(w http.ResponseWriter, req *http.Request) {
			gotID, gotOK = ParseUUIDParam(w, req, "id", label...)
		})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		return gotID, gotOK, rec
	}

	t.Run("valid UUID", func(t *testing.T) {
		want := uuid.New()
		id, ok, rec := serve("/things/{id}", "/things/"+want.String())
		if !ok || id != want {
			t.Errorf("got (%v, %v), want (%v, true)", id, ok, want)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("invalid UUID uses the key in the message", func(t *testing.T) {
		id, ok, rec := serve("/things/{id}", "/things/not-a-uuid")
		if ok || id != uuid.Nil {
			t.Errorf("got (%v, %v), want (nil, false)", id, ok)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "invalid id" {
			t.Errorf("body = %q, want %q", got, "invalid id")
		}
	})

	t.Run("invalid UUID uses the label when given", func(t *testing.T) {
		_, ok, rec := serve("/things/{id}", "/things/nope", "provider id")
		if ok {
			t.Error("expected ok=false")
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "invalid provider id" {
			t.Errorf("body = %q, want %q", got, "invalid provider id")
		}
	})
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, "api", map[string]string{"hello": "world"})
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["hello"] != "world" {
		t.Errorf("body = %v", body)
	}
}

// TestWriteJSON_WriteFailureIsLogOnly pins the post-commit half of the split:
// the value marshalled, so bytes were on their way out and the status line is
// already gone. A second WriteHeader could never reach the client, so the
// failure is logged and nothing else.
func TestWriteJSON_WriteFailureIsLogOnly(t *testing.T) {
	capt := captureLogs(t)
	w := newErrWriter(errors.New("boom"))
	WriteJSON(w, "api", map[string]string{"a": "b"})
	if capt.msg != "api: failed to encode JSON response" {
		t.Errorf("log message = %q", capt.msg)
	}
	if capt.last != slog.LevelError {
		t.Errorf("log level = %v, want error", capt.last)
	}
	if w.code != 0 {
		t.Errorf("wrote status %d after the body started; want none", w.code)
	}
}

// TestWriteJSON_MarshalFailureIs500 pins the pre-commit half: json.Marshal
// fails having written nothing, so a real 500 is still deliverable and must be
// sent rather than letting net/http emit a 200 with an empty body.
func TestWriteJSON_MarshalFailureIs500(t *testing.T) {
	capt := captureLogs(t)
	w := httptest.NewRecorder()
	WriteJSON(w, "api", map[string]any{"ch": make(chan int)})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "failed to encode response" {
		t.Errorf("body = %q, want the sanitized encode-failure message", got)
	}
	if capt.msg != "api: failed to encode response" {
		t.Errorf("log message = %q", capt.msg)
	}
	if capt.last != slog.LevelError {
		t.Errorf("log level = %v, want error", capt.last)
	}
}

// TestWriteJSONStatus_MarshalFailureKeepsStatus pins the deliberate difference
// from WriteJSON: the caller already chose a status that carries the meaning
// (409 duplicate, 422 schema mismatch), so an unmarshalable body loses the body
// and nothing more.
func TestWriteJSONStatus_MarshalFailureKeepsStatus(t *testing.T) {
	capt := captureLogs(t)
	w := httptest.NewRecorder()
	WriteJSONStatus(w, "api", http.StatusConflict, map[string]any{"ch": make(chan int)})
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want the caller's 409", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
	if capt.msg != "api: failed to encode JSON response" {
		t.Errorf("log message = %q", capt.msg)
	}
	if capt.last != slog.LevelError {
		t.Errorf("log level = %v, want error", capt.last)
	}
}

func TestWriteJSONStatus(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSONStatus(w, "api", http.StatusCreated, map[string]int{"n": 1})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if strings.TrimSpace(w.Body.String()) != `{"n":1}` {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestWriteJSONStatus_EncodeFailureIsLogged(t *testing.T) {
	capt := captureLogs(t)
	w := newErrWriter(syscall.EPIPE)
	WriteJSONStatus(w, "frontdesk", http.StatusOK, map[string]int{"n": 1})
	if w.code != http.StatusOK {
		t.Errorf("WriteHeader got %d, want 200", w.code)
	}
	if capt.last != slog.LevelDebug {
		t.Errorf("log level = %v, want debug for a client disconnect", capt.last)
	}
	if capt.msg != "frontdesk: client disconnected before JSON response completed" {
		t.Errorf("log message = %q", capt.msg)
	}
}

func TestWriteCodedError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteCodedError(w, "api", http.StatusConflict, "duplicate", "already exists")
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["code"] != "duplicate" || body["error"] != "already exists" {
		t.Errorf("body = %v, want {code, error}", body)
	}
}

func TestIsClientDisconnect(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"broken pipe", syscall.EPIPE, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"closed conn", net.ErrClosed, true},
		{"context canceled is not a disconnect", context.Canceled, false},
		{"wrapped broken pipe", fmt.Errorf("write tcp: %w", syscall.EPIPE), true},
		{"unmarshalable value", errors.New("json: unsupported type: chan int"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsClientDisconnect(tc.err); got != tc.want {
				t.Errorf("IsClientDisconnect(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestLogEncodeError_Level(t *testing.T) {
	capt := captureLogs(t)

	t.Run("client disconnect logs at debug", func(t *testing.T) {
		capt.last = slog.LevelError + 1 // sentinel
		LogEncodeError("api", fmt.Errorf("write tcp 1.2.3.4:8080->5.6.7.8:9: write: %w", syscall.EPIPE))
		if capt.last != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", capt.last)
		}
	})

	t.Run("handler timeout logs at debug", func(t *testing.T) {
		capt.last = slog.LevelError + 1 // sentinel
		LogEncodeError("api", fmt.Errorf("encode: %w", http.ErrHandlerTimeout))
		if capt.last != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", capt.last)
		}
		if capt.msg != "api: handler timed out before JSON response completed" {
			t.Errorf("log message = %q", capt.msg)
		}
	})

	t.Run("genuine encode error logs at error", func(t *testing.T) {
		capt.last = slog.LevelDebug - 1 // sentinel
		LogEncodeError("api", errors.New("json: unsupported type: chan int"))
		if capt.last != slog.LevelError {
			t.Errorf("expected error level, got %v", capt.last)
		}
	})
}

// TestReadOnlyGuard verifies the middleware in isolation: safe methods reach the
// next handler, mutating methods are refused with 403 and never reach it.
func TestReadOnlyGuard(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	guard := ReadOnlyGuard("api", next)

	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(m, "/providers", http.NoBody))
		if !called {
			t.Errorf("%s: expected next handler to be called", m)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", m, rec.Code)
		}
	}

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(m, "/providers", http.NoBody))
		if called {
			t.Errorf("%s: next handler must not be called in read-only mode", m)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", m, rec.Code)
		}
	}

	// The exempt POSTs mutate no catalog or credential data, so they pass
	// through even in read-only mode.
	for _, path := range []string{"/api/discovery/changes/ack", "/api/auth/webauthn/logout"} {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, http.NoBody))
		if !called {
			t.Errorf("POST %s: expected exemption to reach next handler", path)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("POST %s: expected 200, got %d", path, rec.Code)
		}
	}

	// Dismiss is NOT exempt. It suppresses a real discrepancy from everyone's
	// view, unlike the ack it sits next to, which only flips a per-row seen
	// flag. Pattern-matching the neighbouring exemption is the easy way to get
	// this wrong, so it is asserted explicitly.
	called = false
	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/dismiss", http.NoBody))
	if called {
		t.Error("POST /api/discovery/dismiss: next handler must not be called in read-only mode")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("read-only POST /api/discovery/dismiss = %d, want 403", rec.Code)
	}
}

func TestIsReadOnlyExemptPost(t *testing.T) {
	cases := map[string]bool{
		"/api/discovery/changes/ack":  true,
		"/discovery/changes/ack":      true,
		"/api/auth/webauthn/logout":   true,
		"/api/discovery/dismiss":      false,
		"/api/providers":              false,
		"/api/discovery/changes/acks": false,
	}
	for path, want := range cases {
		if got := IsReadOnlyExemptPost(path); got != want {
			t.Errorf("IsReadOnlyExemptPost(%q) = %v, want %v", path, got, want)
		}
	}
}
