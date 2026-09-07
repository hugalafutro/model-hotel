// Package egress holds the pieces the vendor dialect translators share.
//
// Each translator (anthropic, anthropicegress, gemini, openairesponses) maps an
// OpenAI-shaped request onto one vendor's wire format and the reply back again.
// They are deliberately independent of each other, but the mechanical parts —
// reading OpenAI's string-or-array union fields, and re-framing an upstream SSE
// body as chat.completion.chunk bytes — are the same work in every one of them,
// so they live here once.
package egress

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// AsJSONString returns the value when raw is a JSON string literal, and
// ok=false for arrays, objects and an absent field. JSON null decodes into a
// string without error, so it yields ("", true) — which every caller wants,
// since a null content field carries nothing either way. Used to tell
// plain-string message content from a content-part array.
func AsJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

// DecodeStop accepts OpenAI's string-or-array stop field. An empty string is
// not a stop sequence, so it decodes to nil rather than to a one-element list
// that would truncate the completion immediately.
func DecodeStop(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	if s, ok := AsJSONString(raw); ok {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	return nil
}

// FlattenText reduces an OpenAI content field to plain text: a JSON string
// verbatim, or the concatenated text of a content-part array (parts with no
// type, or type "text"; every other part carries something text cannot hold).
// ok is false when raw is neither, which includes an absent or null field.
func FlattenText(raw json.RawMessage) (string, bool) {
	if s, ok := AsJSONString(raw); ok {
		return s, true
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), true
}

// DecodeToolChoice reads the OpenAI chat tool_choice union. mode is one of
// "auto", "none", "required" (the string forms) or "function", in which case
// name carries the requested function. ok is false for an absent field and
// for a value neither shape covers, which every dialect treats as "no choice
// stated" rather than as an error.
func DecodeToolChoice(raw json.RawMessage) (mode, name string, ok bool) {
	if len(raw) == 0 {
		return "", "", false
	}
	if s, isString := AsJSONString(raw); isString {
		switch s {
		case "auto", "none", "required":
			return s, "", true
		}
		return "", "", false
	}
	var tc struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &tc) == nil && tc.Function.Name != "" {
		return "function", tc.Function.Name, true
	}
	return "", "", false
}

// NewChatCompletionID mints the id every chat-completion body and chunk
// carries, in the shape OpenAI uses: "chatcmpl-" and a bare hex UUID.
func NewChatCompletionID() string {
	return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}
