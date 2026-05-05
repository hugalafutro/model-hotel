package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SanitizeLogBody redacts potentially sensitive information from upstream
// API error response bodies before they are written to logs or stored in the
// database. It truncates the body to maxLen characters and replaces UUIDs
// with [REDACTED], as upstream error messages often leak internal identifiers
// (team IDs, project IDs) that have no diagnostic value.
// uuidPattern matches standard UUIDs (e.g., 793ac38b-0211-43e6-baa7-aa7054c39931)
// which upstream providers often include in error messages (team IDs, project IDs, etc.).
var uuidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func SanitizeLogBody(body string, maxLen int) string {
	if len(body) > maxLen {
		// Back up to the last valid UTF-8 rune boundary to avoid splitting multi-byte characters
		for len(body) > maxLen {
			_, size := utf8.DecodeLastRuneInString(body)
			body = body[:len(body)-size]
		}
		body = body + "…"
	}
	return uuidPattern.ReplaceAllString(body, "[REDACTED]")
}

// ParseBearerToken extracts the token from an Authorization: Bearer <token> header.
// Returns the token and true if valid, or empty string and false if the header
// is missing or malformed.
func ParseBearerToken(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", false
	}
	if len(authHeader) <= 7 || authHeader[:7] != "Bearer " {
		return "", false
	}
	token := authHeader[7:]
	if token == "" {
		return "", false
	}
	return token, true
}

// ParseUUIDParam extracts and parses a UUID from a chi URL parameter.
// Returns the parsed UUID or an error.
func ParseUUIDParam(r *http.Request, key string) (uuid.UUID, error) {
	idStr := chi.URLParam(r, key)
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return id, nil
}

// GetIntQueryParam parses an integer from a query parameter, returning
// defaultValue if the parameter is missing or unparseable.
func GetIntQueryParam(r *http.Request, key string, defaultValue int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultValue
	}
	result, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return result
}

// IntToStr converts an integer to its string representation.
// This is a convenience function primarily useful for building
// parameterized SQL queries with positional arguments.
func IntToStr(i int) string {
	return strconv.Itoa(i)
}

// WriteOpenAIError writes an OpenAI-compatible JSON error response.
// All proxy-path error responses must be JSON, not plain text, because
// clients like SillyTavern parse responses as JSON and crash on plain
// text error messages.
func WriteOpenAIError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    OpenAIErrorType(statusCode),
			"code":    statusCode,
		},
	})
}

// OpenAIErrorType maps an HTTP status code to the corresponding OpenAI error type string.
func OpenAIErrorType(code int) string {
	switch {
	case code == 401:
		return "authentication_error"
	case code == 403:
		return "permission_error"
	case code == 404:
		return "not_found_error"
	case code == 429:
		return "rate_limit_error"
	case code >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}
