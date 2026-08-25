// Package egress holds the pieces the vendor dialect translators share.
//
// Each translator (anthropic, anthropicegress, gemini, openairesponses) maps an
// OpenAI-shaped request onto one vendor's wire format and the reply back again.
// They are deliberately independent of each other, but the mechanical parts —
// reading OpenAI's string-or-array union fields, and re-framing an upstream SSE
// body as chat.completion.chunk bytes — are the same work in every one of them,
// so they live here once.
package egress

import "encoding/json"

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
