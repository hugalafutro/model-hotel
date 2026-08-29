package util

import (
	"encoding/json"
	"strings"
)

// ToolArguments is an OpenAI tool-call arguments value.
//
// The spec says a JSON string. Several providers send the object itself, and a
// field typed as a plain string fails to decode it — which, in a struct decoded
// as a whole, discards the entire frame or response along with it. On the
// streaming surface the loss is not even uniform: finish_reason rides a separate
// frame that decodes fine, so the caller is told a tool call happened and never
// receives it.
//
// Decoding accepts either spelling and keeps the argument text. Encoding needs
// no custom method: this is a named string type, so it marshals to the spec's
// JSON string on its own, and a provider's non-standard spelling is absorbed
// wherever the value is re-encoded rather than relayed.
//
// The streaming surface relays bytes rather than re-encoding, so it normalises
// explicitly — see normalizeToolArguments. That normalisation is conformance
// rather than repair: every decoder in this repo reads either spelling now.
type ToolArguments string

// UnmarshalJSON accepts the spec's JSON string or the argument object itself.
// A JSON null decodes to the empty string, which is the right reading: a null
// arguments member carries none.
func (a *ToolArguments) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*a = ToolArguments(s)
		return nil
	}
	// The object (or array, or number) form: its own JSON is the argument text.
	*a = ToolArguments(b)
	return nil
}

// ToolArgumentsObject renders tool-call arguments as the JSON OBJECT the
// dialects that type them as one require.
//
// Anything that is not one becomes an empty object. That is this repo's existing
// decision, not a new one — see TestTranslateRequest_ToolCallArgumentsBecomeAnObject:
// a model that emits junk arguments mid-conversation should not kill the whole
// request, and a call with no arguments is still a call.
//
// One helper because the three egress translators each made that decision for
// themselves and each made it differently: an array became {} for Anthropic, was
// forwarded to Gemini as a non-Struct `args` it answers 400 to, and reached the
// Responses API as a quoted array the model reads as garbage. Which of those a
// caller met depended on which member of a failover group the turn landed on —
// and removing exactly that divergence is what these arguments are decoded
// tolerantly for. The coercion was never the defect; three of them were.
func ToolArgumentsObject(a ToolArguments) json.RawMessage {
	raw := json.RawMessage(strings.TrimSpace(string(a)))
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}
