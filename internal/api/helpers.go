package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// logComponent is the log prefix every helper in this package writes.
const logComponent = "api"

// respondError logs the error details server-side and sends an HTTP error response.
// Internal error details are logged but never sent to the client.
func respondError(w http.ResponseWriter, message string, err error, code int) {
	httpx.RespondError(w, logComponent, message, err, code)
}

// respondLookupError maps a repository lookup error to an HTTP response: a
// genuine miss becomes a 404, any other error a logged 500.
func respondLookupError(w http.ResponseWriter, err, notFound error, notFoundMsg, loadMsg string) {
	httpx.RespondLookupError(w, logComponent, err, notFound, notFoundMsg, loadMsg)
}

// respondBadRequest sends a 400 response with a sanitized message.
func respondBadRequest(w http.ResponseWriter, message string, err error) {
	httpx.RespondBadRequest(w, logComponent, message, err)
}

// parseUUIDParam extracts and validates a UUID from the chi URL params.
// The optional label parameter customizes the error message (defaults to key).
func parseUUIDParam(w http.ResponseWriter, r *http.Request, key string, label ...string) (uuid.UUID, bool) {
	return httpx.ParseUUIDParam(w, r, key, label...)
}

// writeJSON sets the Content-Type header and encodes the response as JSON.
func writeJSON(w http.ResponseWriter, v any) {
	httpx.WriteJSON(w, logComponent, v)
}

// writeJSONStatus writes v as JSON with an explicit status code.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSONStatus(w, logComponent, status, v)
}

// writeJSONCreated sets the Content-Type header, writes 201 status, and encodes the response.
func writeJSONCreated(w http.ResponseWriter, v any) {
	httpx.WriteJSONStatus(w, logComponent, http.StatusCreated, v)
}

// writeCodedError writes a JSON {code, error} body so the dashboard can route
// on a stable code instead of matching English text.
func writeCodedError(w http.ResponseWriter, status int, code, msg string) {
	httpx.WriteCodedError(w, logComponent, status, code, msg)
}

// decodeJSON bounds the request body to httpx.MaxJSONBody and decodes it,
// writing the 400/413 response itself and reporting whether the handler may
// continue. Every handler in this package decodes through here (or through
// decodeJSONLimit) so no route can be added with an unbounded body, which
// internal/httpx's TestNoUnboundedJSONDecode guard enforces.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return httpx.DecodeJSON(w, r, logComponent, httpx.MaxJSONBody, v)
}

// decodeJSONLimit is decodeJSON with an endpoint-specific ceiling, for the two
// routes whose payload is legitimately larger or smaller than the default: a
// config-sync import and the fleet announce heartbeat.
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	return httpx.DecodeJSON(w, r, logComponent, limit, v)
}

// decodeJSONOptional is decodeJSON for a body the endpoint treats as optional:
// a missing or malformed body leaves the request struct at its defaults, but an
// oversized one is still refused.
func decodeJSONOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	return httpx.DecodeJSONOptional(w, r, logComponent, httpx.MaxJSONBody, v)
}
