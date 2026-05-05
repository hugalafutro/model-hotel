package api

import (
	"encoding/json"
	"net/http"

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
		debuglog.Error("api: failed to encode JSON response", "error", err)
	}
}

// writeJSONCreated sets the Content-Type header, writes 201 status, and encodes the response.
func writeJSONCreated(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debuglog.Error("api: failed to encode JSON response", "error", err)
	}
}
