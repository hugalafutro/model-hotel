package anthropic

import (
	"encoding/json"
	"net/http"
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

// openAIErrorBody is the shape util.WriteOpenAIError emits. `code` is left out
// deliberately: the proxy writes it as an int, so typing it here (as string or
// int) risks an unmarshal mismatch that would discard the whole envelope and
// leak the raw JSON as the message. We only need the message.
type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// BuildErrorResponse produces an Anthropic-shaped error body
// ({"type":"error","error":{"type","message"}}) for the given HTTP status. When
// openaiBody carries an OpenAI error envelope, its message is reused; otherwise
// the raw body (or a status-derived default) becomes the message. The error
// `type` is always derived from the status so it matches Anthropic's vocabulary.
func BuildErrorResponse(openaiBody []byte, status int) []byte {
	message := ""
	var oaErr openAIErrorBody
	if json.Unmarshal(openaiBody, &oaErr) == nil && oaErr.Error.Message != "" {
		message = oaErr.Error.Message
	} else if len(openaiBody) > 0 {
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
