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
	"io"
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

// marshalBody renders v as a response body. Marshalling separately from writing
// is what lets the writers below tell the two failure modes apart: a marshal
// failure (an unmarshalable value — NaN/Inf, a chan or func field, a cycle) has
// put nothing on the wire, so a real status is still deliverable, whereas a
// write failure has already committed the response. The trailing newline
// matches what json.Encoder.Encode emits, which callers and tests rely on.
func marshalBody(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteJSON sets the Content-Type header and writes v as a JSON body with an
// implicit 200. A value that cannot be marshalled is a server fault caught
// before any byte is written, so it becomes a logged 500; a write that fails
// mid-body is only logged, since the response is already committed and a second
// WriteHeader could never reach the client.
func WriteJSON(w http.ResponseWriter, component string, v any) {
	b, err := marshalBody(v)
	if err != nil {
		RespondError(w, component, "failed to encode response", err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(b); err != nil {
		LogEncodeError(component, err)
	}
}

// WriteJSONStatus sets the Content-Type header, writes an explicit status code,
// and writes v as the JSON body. Unlike WriteJSON it keeps the caller's status
// when the value cannot be marshalled: that status is usually the whole message
// (a 409 the dashboard routes on, a 422 the config-sync import checks), so
// replacing it with a 500 would lose more than the body already lost. Either
// failure is logged.
func WriteJSONStatus(w http.ResponseWriter, component string, status int, v any) {
	b, err := marshalBody(v)
	if err != nil {
		LogEncodeError(component, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if b == nil {
		return
	}
	if _, err := w.Write(b); err != nil {
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

// MaxJSONBody is the default ceiling on a control-plane JSON request body.
// Every admin, auth and Front Desk payload is a small settings/credential/ID
// document, so a megabyte is orders of magnitude more than any of them needs
// while still bounding what an unauthenticated caller (a login attempt, a
// device pairing exchange) can make the server read and buffer. The largest
// legitimate payload on any route that uses it is a full bulk model delete, at
// roughly 400 KiB; TestBulkDeleteWorstCaseFitsDefaultLimit pins that headroom
// so raising the ID cap cannot silently outgrow this one. Endpoints that
// legitimately carry more (a config-sync import, a backup upload) pass their
// own limit instead of using this one.
//
// This is a second, much tighter ceiling on the main server, whose router
// already caps every request at MAX_REQUEST_SIZE (50 MB by default) for the
// sake of the proxy's multimodal uploads: 50 MB is the right bound for a
// base64 image and a badly wrong one for a login body. The Front Desk binary
// mounts no such global cap at all, so for its handlers this is the only
// ceiling there is.
const MaxJSONBody = 1 << 20 // 1 MiB

// DecodeJSON bounds r.Body to limit bytes and decodes exactly one JSON value
// into v, reporting whether the caller may continue. On failure it has already
// written the response: 413 when the body ran past the limit, 400 when it is
// malformed or carries anything after that value.
//
// The bound is applied with http.MaxBytesReader rather than by checking
// Content-Length, so a chunked or lying request is cut off mid-read instead of
// being trusted. MaxBytesReader also tries to tell the ResponseWriter the
// request was oversized, but it does that through an unexported interface that
// no wrapped writer in either binary implements (chi's logger and compressor
// both wrap it), so in practice the connection is closed by net/http's own
// post-handler drain instead. Either way the handler never reads past the
// limit, which is the property that matters.
func DecodeJSON(w http.ResponseWriter, r *http.Request, component string, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return rejectDecode(w, r, component, limit, err)
	}
	return checkNothingFollows(w, r, component, limit, dec)
}

// DecodeJSONOptional is DecodeJSON for an endpoint whose body is optional: no
// body at all leaves v at its zero value and the request continues, so the
// caller keeps its documented default.
//
// Only an entirely absent body is tolerated. A body that is present but
// malformed is rejected exactly as DecodeJSON rejects it, because "the client
// sent nothing, so use the default" and "the client asked for something and we
// could not read it, so use the default" are different situations, and
// treating the second as the first silently substitutes the default for what
// was actually asked. On a purge endpoint that is the difference between
// deleting an hour of logs and deleting all of them.
func DecodeJSONOptional(w http.ResponseWriter, r *http.Request, component string, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(v)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
		return rejectDecode(w, r, component, limit, err)
	}
	return checkNothingFollows(w, r, component, limit, dec)
}

// checkNothingFollows rejects a request that carries anything after its one
// JSON value. It is what makes the ceiling real rather than nominal:
// json.Decoder stops reading the moment it has a complete value, so a small
// object followed by a huge tail would decode happily and never trip
// MaxBytesReader. Draining to the end forces the tail through the limit, and a
// tail that is under the limit but non-empty is a smuggled second payload, so
// it earns the same 400 a malformed body does.
//
// Trailing whitespace is not a tail: Token skips it, and a client that ends its
// body with a newline is well behaved.
func checkNothingFollows(w http.ResponseWriter, r *http.Request, component string, limit int64, dec *json.Decoder) bool {
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return rejectDecode(w, r, component, limit, err)
	}
	return true
}

// rejectDecode writes the response for a failed decode and always reports
// false, so a caller can return it directly. An oversized body is a 413;
// anything else is a 400.
//
// Neither log line carries any of the body. The obvious thing to log is the
// decoder error, but json.SyntaxError's message embeds the offending byte, and
// these routes include the pre-authentication ones, so that would put
// caller-controlled content into the log of a gateway whose whole premise is
// that request content is never logged. Normalising every failure to a fixed
// phrase means no decoder message can reach a log line by accident either. The
// phrase and the byte offset say everything an operator needs.
func rejectDecode(w http.ResponseWriter, r *http.Request, component string, limit int64, err error) bool {
	if isBodyTooLarge(err) {
		debuglog.Info(component+": rejected oversized request body",
			"path", r.URL.Path, "method", r.Method, "limit_bytes", limit)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return false
	}
	kind, offset := decodeFailure(err)
	debuglog.Info(component+": rejected malformed request body",
		"path", r.URL.Path, "method", r.Method, "reason", kind, "offset", offset)
	http.Error(w, "invalid request body", http.StatusBadRequest)
	return false
}

// decodeFailure classifies a decode failure into a fixed phrase and a byte
// offset, both safe to log: the phrase is one of these constants and the offset
// is a position, never a value from the body. A nil error is the
// something-follows case, where the decoder read a valid token that should not
// have been there.
func decodeFailure(err error) (kind string, offset int64) {
	var syntax *json.SyntaxError
	var wrongType *json.UnmarshalTypeError
	switch {
	case err == nil:
		return "trailing data after JSON value", 0
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated JSON value", 0
	case errors.Is(err, io.EOF):
		return "empty body", 0
	case errors.As(err, &syntax):
		return "malformed JSON", syntax.Offset
	case errors.As(err, &wrongType):
		return "wrong JSON type for field", wrongType.Offset
	default:
		return "unreadable body", 0
	}
}

// isBodyTooLarge reports whether err is MaxBytesReader's over-the-limit error.
// json.Decoder surfaces a read error unwrapped, but errors.As keeps this
// correct if a future decode path wraps it.
func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
