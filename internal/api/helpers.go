package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// respondError logs the error details server-side and sends an HTTP error response.
// Internal error details are logged but never sent to the client.
func respondError(w http.ResponseWriter, message string, err error, code int) {
	if err != nil {
		log.Printf("[api] error: %s: %v", message, err)
	}
	http.Error(w, message, code)
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
		log.Printf("[api] error: failed to encode JSON response: %v", err)
	}
}

// writeJSONCreated sets the Content-Type header, writes 201 status, and encodes the response.
func writeJSONCreated(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] error: failed to encode JSON response: %v", err)
	}
}
