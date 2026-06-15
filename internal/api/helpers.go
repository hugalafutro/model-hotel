package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// respondError logs the error details server-side and sends an HTTP error response.
// Internal error details are logged but never sent to the client.
// For 5xx errors without an error value, the message is still logged for debugging.
func respondError(w http.ResponseWriter, message string, err error, code int) {
	if err != nil {
		debuglog.Error("api: "+message, "error", err)
	} else if code >= 500 {
		debuglog.Error("api: " + message)
	}
	http.Error(w, message, code)
}

// respondLookupError maps a repository lookup error to an HTTP response: a
// genuine miss (err matching the notFound sentinel) becomes a 404 with
// notFoundMsg; any other error becomes a logged 500 with loadMsg. This keeps a
// database outage from being silently reported to the client as "not found".
func respondLookupError(w http.ResponseWriter, err, notFound error, notFoundMsg, loadMsg string) {
	if errors.Is(err, notFound) {
		http.Error(w, notFoundMsg, http.StatusNotFound)
		return
	}
	respondError(w, loadMsg, err, http.StatusInternalServerError)
}

// respondBadRequest sends a 400 response with a sanitized message.
// If err is non-nil, the error details are logged server-side only.
func respondBadRequest(w http.ResponseWriter, message string, err error) {
	if err != nil {
		debuglog.Info("api: bad request: "+message, "error", err)
	}
	http.Error(w, message, http.StatusBadRequest)
}

// parseUUIDParam extracts and validates a UUID from the chi URL params.
// The optional label parameter customizes the error message (defaults to key).
func parseUUIDParam(w http.ResponseWriter, r *http.Request, key string, label ...string) (uuid.UUID, bool) {
	idStr := chi.URLParam(r, key)
	id, err := uuid.Parse(idStr)
	if err != nil {
		name := key
		if len(label) > 0 {
			name = label[0]
		}
		http.Error(w, "invalid "+name, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

// writeJSON sets the Content-Type header and encodes the response as JSON.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logEncodeError(err)
	}
}

// writeJSONCreated sets the Content-Type header, writes 201 status, and encodes the response.
func writeJSONCreated(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logEncodeError(err)
	}
}

// logEncodeError logs a failure to encode a JSON response. A client that hangs
// up before the body is written (broken pipe, connection reset, closed conn) is
// not a server fault, so it is logged at debug level to keep production logs
// clean; any other failure (e.g. an unmarshalable value) stays at error level
// so genuine bugs remain visible even with debug disabled.
func logEncodeError(err error) {
	if isClientDisconnect(err) {
		debuglog.Debug("api: client disconnected before JSON response completed", "error", err)
		return
	}
	debuglog.Error("api: failed to encode JSON response", "error", err)
}

// isClientDisconnect reports whether err indicates the client closed the
// connection before the response could be fully written. These are the
// OS-level write errors that unambiguously signal a dead client TCP connection;
// context cancellation is deliberately excluded because it crosses a different
// boundary (a server-side cancel must not be silently downgraded), and the
// response-encode path produces these write errors, not context.Canceled.
func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed)
}
