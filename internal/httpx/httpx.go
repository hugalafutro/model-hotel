// Package httpx holds the HTTP response helpers shared by the admin surfaces
// (internal/api, internal/adminauth, internal/frontdesk). Each of those
// packages previously carried its own copy of these bodies; the copies existed
// only to avoid an import dependency between the packages, so a neutral
// leaf package removes the reason for them.
//
// Every helper that logs takes a component prefix ("api", "adminauth",
// "frontdesk") so the log lines keep naming the surface that produced them.
package httpx

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// RespondError logs the error details server-side and sends an HTTP error
// response. Internal error details are logged but never sent to the client.
// For 5xx errors without an error value, the message is still logged for
// debugging.
func RespondError(w http.ResponseWriter, component, message string, err error, code int) {
	if err != nil {
		debuglog.Error(component+": "+message, "error", err)
	} else if code >= 500 {
		debuglog.Error(component + ": " + message)
	}
	http.Error(w, message, code)
}

// RespondLookupError maps a repository lookup error to an HTTP response: a
// genuine miss (err matching the notFound sentinel) becomes a 404 with
// notFoundMsg; any other error becomes a logged 500 with loadMsg. This keeps a
// database outage from being silently reported to the client as "not found".
func RespondLookupError(w http.ResponseWriter, component string, err, notFound error, notFoundMsg, loadMsg string) {
	if errors.Is(err, notFound) {
		http.Error(w, notFoundMsg, http.StatusNotFound)
		return
	}
	RespondError(w, component, loadMsg, err, http.StatusInternalServerError)
}

// RespondBadRequest sends a 400 response with a sanitized message.
// If err is non-nil, the error details are logged server-side only.
func RespondBadRequest(w http.ResponseWriter, component, message string, err error) {
	if err != nil {
		debuglog.Info(component+": bad request: "+message, "error", err)
	}
	http.Error(w, message, http.StatusBadRequest)
}

// ParseUUIDParam extracts and validates a UUID from the chi URL params.
// The optional label parameter customizes the error message (defaults to key).
func ParseUUIDParam(w http.ResponseWriter, r *http.Request, key string, label ...string) (uuid.UUID, bool) {
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

// WriteJSON sets the Content-Type header and encodes the response as JSON.
func WriteJSON(w http.ResponseWriter, component string, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		LogEncodeError(component, err)
	}
}

// WriteJSONStatus sets the Content-Type header, writes an explicit status code,
// and encodes the response as JSON.
func WriteJSONStatus(w http.ResponseWriter, component string, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		LogEncodeError(component, err)
	}
}

// WriteCodedError writes a JSON {code, error} body so the dashboard can route
// on a stable code instead of matching English text.
func WriteCodedError(w http.ResponseWriter, component string, status int, code, msg string) {
	WriteJSONStatus(w, component, status, map[string]string{"code": code, "error": msg})
}

// LogEncodeError logs a failure to encode a JSON response. A client that hangs
// up before the body is written (broken pipe, connection reset, closed conn)
// and a handler whose deadline fired mid-response (http.ErrHandlerTimeout) are
// not server faults, so they are logged at debug level to keep production logs
// clean; any other failure (e.g. an unmarshalable value) stays at error level
// so genuine bugs remain visible even with debug disabled.
func LogEncodeError(component string, err error) {
	switch {
	case IsClientDisconnect(err):
		debuglog.Debug(component+": client disconnected before JSON response completed", "error", err)
	case errors.Is(err, http.ErrHandlerTimeout):
		debuglog.Debug(component+": handler timed out before JSON response completed", "error", err)
	default:
		debuglog.Error(component+": failed to encode JSON response", "error", err)
	}
}

// IsClientDisconnect reports whether err indicates the client closed the
// connection before the response could be fully written. These are the
// OS-level write errors that unambiguously signal a dead client TCP connection;
// context cancellation is deliberately excluded because it crosses a different
// boundary (a server-side cancel must not be silently downgraded), and the
// response-encode path produces these write errors, not context.Canceled.
func IsClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed)
}

// ReadOnlyGuard rejects state-changing requests when the instance runs in
// read-only mode (DEMO_READONLY=true). Safe methods (GET/HEAD/OPTIONS) pass
// through so the dashboard stays fully browsable; every mutating method —
// create, update, delete — gets a 403.
//
// It is mounted only on the admin CRUD routers, so the admin chat (/api/chat)
// and the public proxy (/v1) are deliberately unaffected: a demo visitor can
// still chat against the seeded providers and use a seeded virtual key, they
// just cannot add, edit, or delete anything.
//
// One exception: acknowledging background-discovery notifications. That POST
// only flips a per-row "seen" flag on the discovery_changes table — it does not
// touch the model catalog — so it is allowed even in read-only mode. Without
// this, a demo instance can show the Models nav badge but never clear it, and it
// reappears on every poll/reload.
func ReadOnlyGuard(component string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
		case http.MethodPost:
			if IsReadOnlyExemptPost(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			fallthrough
		default:
			// nil err + a 4xx code: RespondError does not log this (it is a
			// client-facing policy rejection, not a server fault).
			RespondError(w, component, "this is a read-only demo: creating, editing, and deleting are disabled", nil, http.StatusForbidden)
		}
	})
}

// IsReadOnlyExemptPost reports whether a POST path stays allowed in read-only
// mode. These mutate no catalog or credential data. Matched by suffix so they
// are independent of the router's mount prefix:
//   - discovery-change acknowledgement (flips a per-row "seen" flag), and
//   - WebAuthn logout (revokes the current session only; it is not admin
//     credential management like registering or deleting a passkey).
func IsReadOnlyExemptPost(path string) bool {
	return strings.HasSuffix(path, "/discovery/changes/ack") ||
		strings.HasSuffix(path, "/webauthn/logout")
}
