package util

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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
