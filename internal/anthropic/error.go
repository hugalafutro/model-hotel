package anthropic

import (
	"encoding/json"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// anthropicErrorType maps an HTTP status code to the Anthropic error `type`
// vocabulary (invalid_request_error, authentication_error, permission_error,
// not_found_error, rate_limit_error, api_error, overloaded_error).
func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

// BuildErrorResponse produces an Anthropic-shaped error body
// ({"type":"error","error":{"type","message"}}) for the given HTTP status. When
// openaiBody carries an OpenAI error envelope, its message is reused; otherwise
// the raw body (or a status-derived default) becomes the message. The error
// `type` is always derived from the status so it matches Anthropic's vocabulary.
func BuildErrorResponse(openaiBody []byte, status int) []byte {
	// util.ErrorEnvelopeMessage reads only the message. The proxy writes the
	// envelope's `code` as an int, so a struct typing it here (as string or
	// int) would risk an unmarshal mismatch that discards the whole envelope
	// and leaks the raw JSON as the message.
	message := util.ErrorEnvelopeMessage(openaiBody)
	if message == "" && len(openaiBody) > 0 {
		message = string(openaiBody)
	}
	return BuildErrorResponseFromMessage(message, status)
}

// BuildErrorResponseFromMessage produces an Anthropic-shaped error body from a
// plain message and HTTP status. An empty message defaults to the status text.
func BuildErrorResponseFromMessage(message string, status int) []byte {
	if message == "" {
		message = http.StatusText(status)
	}
	ev := errorEvent{
		Type: "error",
		Error: errorPayload{
			Type:    anthropicErrorType(status),
			Message: message,
		},
	}
	out, err := json.Marshal(ev)
	if err != nil {
		// errorEvent is a fixed, marshalable shape; fall back to a minimal
		// literal if marshaling ever fails so the client still gets valid JSON.
		return []byte(`{"type":"error","error":{"type":"api_error","message":"internal error"}}`)
	}
	return out
}
