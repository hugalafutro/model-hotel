package util

import (
	"encoding/json"
	"strings"
)

// ErrorMemberCarries reports whether the "error" member of a provider payload
// leaves a client something to read.
//
// The test is emptiness, not shape. What a provider puts inside its error is
// not a gateway's to judge, so any populated value counts: an object with
// fields, Ollama's bare string ("model not found"), a list, even a number. What
// does not count is a member that leaves a caller with nothing — null, {}, "",
// [] — because a payload carrying one of those is, from the caller's side, the
// same payload with no error member at all. An absent member (a nil or empty
// raw) carries nothing, which is what makes the missing key and the
// null-valued key the same answer.
//
// `false` and `0` carry nothing either. They are not a peculiar error but the C
// convention for its absence, and they arrive on every frame of every 200
// stream from a relay that uses it. Reading "error":false as a failure marks
// every one of those requests failed, suppresses its terminal frame, and loses
// it every hedged race it was in fact winning.
//
// A container carries whatever its values carry, because the same convention
// appears one level down: {"code":0,"message":""} and {"code":null} are a relay
// stamping its no-error struct on every frame, and a rule that stopped at the
// top level called every one of those a provider error.
//
// This lives in util because more than one package reads an error member — the
// OpenAI-compatible streaming path, the hedging probe, the buffered non-2xx
// forwarder, and the native Anthropic passthrough. They read the same bytes, so
// a second opinion is only ever a way for them to disagree, and the disagreement
// costs a hedged race to a provider whose failure is then never recorded.
func ErrorMemberCarries(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var content any
	if err := json.Unmarshal(raw, &content); err != nil {
		return false
	}
	return valueCarries(content)
}

// valueCarries is the emptiness rule over a decoded JSON value. Recursion is
// bounded by the value's own nesting, which encoding/json has already capped
// while building it.
func valueCarries(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case bool:
		return v
	case float64:
		return v != 0
	case map[string]any:
		for _, val := range v {
			if valueCarries(val) {
				return true
			}
		}
		return false
	case []any:
		for _, val := range v {
			if valueCarries(val) {
				return true
			}
		}
		return false
	default:
		// Nothing else survives a decode into `any`; a shape that somehow did
		// is a value the provider put there, and a client can render it.
		return true
	}
}

// ErrorMemberMessage renders the provider's own text out of an error member
// already judged to carry something. The conventional {"message":…} wins; a
// bare string is the message; anything else is rendered as the JSON the
// provider sent, because dropping a frame's only explanation to preserve a
// shape preference helps nobody reading a request log.
//
// The result is raw provider text: unbounded, and scrubbed by nothing. Every
// caller masks and bounds it before storing, forwarding or logging it.
func ErrorMemberMessage(raw json.RawMessage) string {
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	var bare string
	if json.Unmarshal(raw, &bare) == nil {
		return bare
	}
	return string(raw)
}
