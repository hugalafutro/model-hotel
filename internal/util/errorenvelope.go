package util

import (
	"encoding/json"
	"strings"
)

// ErrorEnvelopeMessage extracts the human-readable message from a provider's
// error body. Most providers return a bare object ({"error":{"message":...}}),
// but Google AI Studio wraps the same shape in a one-element array
// ([{"error":{...}}]). Reading only the object form leaves Google's messages
// unparsed, so a rejected param it names can never be learned or stripped.
func ErrorEnvelopeMessage(body []byte) string {
	type errEnvelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	var obj errEnvelope
	if json.Unmarshal(body, &obj) == nil {
		return obj.Error.Message
	}
	var arr []errEnvelope
	if json.Unmarshal(body, &arr) == nil {
		// Joined, not first-wins: a body naming two rejected fields must teach
		// both in one pass rather than costing a 400 round-trip each.
		msgs := make([]string, 0, len(arr))
		for _, e := range arr {
			if e.Error.Message != "" {
				msgs = append(msgs, e.Error.Message)
			}
		}
		return strings.Join(msgs, "; ")
	}
	return ""
}
