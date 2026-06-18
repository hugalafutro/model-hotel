package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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

// SanitizeLogBody truncates and redacts UUIDs from log body strings.
func SanitizeLogBody(body string, maxLen int) string {
	if len(body) > maxLen {
		// Back up to the last valid UTF-8 rune boundary to avoid splitting multi-byte characters
		for len(body) > maxLen {
			_, size := utf8.DecodeLastRuneInString(body)
			body = body[:len(body)-size]
		}
		body += "…"
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

// BuildProviderTargetURL constructs the full upstream URL for a given provider
// and endpoint path (e.g. "/chat/completions", "/embeddings", "/audio/speech").
// Most providers use base + endpoint but Anthropic needs "/v1" + endpoint
// because its base URL (https://api.anthropic.com) lacks the /v1 prefix.
// Defensive: if the base URL already ends with /v1, don't double-append it.
func BuildProviderTargetURL(baseURL, providerType, endpoint string) string {
	sanitized := SanitizeBaseURL(baseURL)
	switch providerType {
	case "anthropic", "ollama", "lmstudio", "koboldcpp":
		// These providers expose their OpenAI-compatible API under /v1: Ollama,
		// LM Studio and KoboldCPP all serve /v1/chat/completions, and Anthropic's
		// compatibility layer lives under /v1. Auto-add the prefix when the
		// configured base URL omits it (e.g. a bare http://host:11434, which
		// discovery accepts but proxying would otherwise 404), and avoid a double
		// /v1 when the user already included it.
		//
		// SanitizeBaseURL only strips a single trailing slash, so normalize any
		// remaining ones here before the suffix check; otherwise an input like
		// http://host/v1// would slip past the guard and get a second /v1.
		versioned := strings.TrimRight(sanitized, "/")
		if strings.HasSuffix(versioned, "/v1") {
			return versioned + endpoint
		}
		return versioned + "/v1" + endpoint
	default:
		// Pass the user's base URL as-is. The user is responsible for
		// including the correct path prefix (e.g. /v1) in the base URL.
		// Known providers already have /v1 in their configured base URLs.
		// Custom providers must be configured with the full path by the user.
		return sanitized + endpoint
	}
}

// SetProviderAuthHeaders sets the correct authentication headers for each provider type.
// - Anthropic: x-api-key + anthropic-version (no Bearer auth)
// - All others: standard Authorization: Bearer header
func SetProviderAuthHeaders(req *http.Request, providerType, apiKey string) {
	if apiKey == "" {
		return
	}
	switch providerType {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
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
