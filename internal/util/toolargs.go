package util

import "encoding/json"

// ToolArguments is an OpenAI tool-call arguments value.
//
// The spec says a JSON string. Several providers send the object itself, and a
// field typed as a plain string fails to decode it — which, in a struct decoded
// as a whole, discards the entire frame or response along with it. On the
// streaming surface the loss is not even uniform: finish_reason rides a separate
// frame that decodes fine, so the caller is told a tool call happened and never
// receives it.
//
// Decoding accepts either spelling and keeps the argument text. Encoding always
// emits the spec string, so a client sees spec-compliant output whatever the
// provider sent, and a provider's non-standard spelling stops at this gateway.
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

// MarshalJSON always writes the spec form, normalising a provider that sent the
// object. Re-encoding it as an object would pass a non-standard shape on to the
// caller, which is the shape this type exists to absorb.
func (a ToolArguments) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(a))
}
