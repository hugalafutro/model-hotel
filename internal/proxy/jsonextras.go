package proxy

import (
	"encoding/json"
	"reflect"
)

// Provider-specific fields that this package does not model must survive the
// decode/re-encode in handleNonStreamingResponse, because some of them are
// required on the NEXT request rather than merely informative.
//
// The case that forced this: Gemini 3 returns each tool call with
// extra_content.google.thought_signature, and rejects the follow-up turn
// outright ("Function call is missing a thought_signature in functionCall
// parts") when the client sends the call back without it. The streaming path
// forwards chunks verbatim and was unaffected; every non-streaming caller lost
// the signature and could not complete a single tool round trip.
//
// Rather than growing a named field per provider extension, the unmodelled keys
// are kept verbatim and re-emitted. Known keys always win, so a field this
// package does model can never be shadowed by a stale copy in the overflow.
type jsonExtras map[string]json.RawMessage

// jsonFieldNames returns the JSON object keys a struct type serialises to.
// Derived by reflection rather than hand-listed so adding a field to Message or
// ToolCall cannot silently desynchronise the overflow from the schema and start
// duplicating that field into the extras map.
func jsonFieldNames(v any) map[string]struct{} {
	t := reflect.TypeOf(v)
	names := make(map[string]struct{}, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		name := tag
		if idx := indexByte(tag, ','); idx >= 0 {
			name = tag[:idx]
		}
		if name == "" {
			name = t.Field(i).Name
		}
		if name == "-" {
			continue
		}
		names[name] = struct{}{}
	}
	return names
}

func indexByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// decodeExtras collects the keys of a JSON object that the caller's struct does
// not model. A payload that is not an object (null, or a provider sending a
// bare string where a message belongs) yields no extras and no error: the
// typed decode alongside it is what decides whether the payload was valid.
func decodeExtras(data []byte, known map[string]struct{}) jsonExtras {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil
	}
	var extras jsonExtras
	for k, v := range all {
		if _, ok := known[k]; ok {
			continue
		}
		if extras == nil {
			extras = make(jsonExtras, len(all))
		}
		extras[k] = v
	}
	return extras
}

// encodeWithExtras marshals base and merges the unmodelled keys back in. Any
// extras key that collides with a modelled one is dropped: the struct field is
// the value this package reasoned about, and re-adding the raw original would
// undo normalisations such as reasoning_content.
func encodeWithExtras(base any, extras jsonExtras) ([]byte, error) {
	out, err := json.Marshal(base)
	if err != nil || len(extras) == 0 {
		return out, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(out, &merged); err != nil {
		return out, nil //nolint:nilerr // a struct that marshalled but will not re-parse is still valid output
	}
	for k, v := range extras {
		if _, clash := merged[k]; clash {
			continue
		}
		merged[k] = v
	}
	return json.Marshal(merged)
}

var (
	messageJSONFields  = jsonFieldNames(Message{})
	toolCallJSONFields = jsonFieldNames(ToolCall{})
	choiceJSONFields   = jsonFieldNames(Choice{})
	responseJSONFields = jsonFieldNames(ChatCompletionResponse{})
	usageJSONFields    = jsonFieldNames(Usage{})
)

// UnmarshalJSON decodes a message and retains any provider fields this package
// does not model (OpenAI's audio and refusal, provider annotations, ...).
func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = Message(a)
	m.Extra = decodeExtras(data, messageJSONFields)
	return nil
}

// MarshalJSON re-emits the message with its unmodelled fields restored.
func (m Message) MarshalJSON() ([]byte, error) {
	type alias Message
	return encodeWithExtras(alias(m), m.Extra)
}

// UnmarshalJSON decodes a tool call and retains the provider fields this
// package does not model, above all Gemini's extra_content.
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	type alias ToolCall
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = ToolCall(a)
	t.Extra = decodeExtras(data, toolCallJSONFields)
	return nil
}

// MarshalJSON re-emits the tool call with its unmodelled fields restored, so a
// client can send the call back to the provider exactly as it was issued.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	type alias ToolCall
	return encodeWithExtras(alias(t), t.Extra)
}

// UnmarshalJSON decodes a choice and retains the per-choice fields this package
// does not model, above all logprobs — a client that asked for them gets
// nothing back without this — and OpenRouter's native_finish_reason.
func (c *Choice) UnmarshalJSON(data []byte) error {
	type alias Choice
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = Choice(a)
	c.Extra = decodeExtras(data, choiceJSONFields)
	return nil
}

// MarshalJSON re-emits the choice with its unmodelled fields restored.
func (c Choice) MarshalJSON() ([]byte, error) {
	type alias Choice
	return encodeWithExtras(alias(c), c.Extra)
}

// UnmarshalJSON decodes a completion and retains the top-level fields this
// package does not model (system_fingerprint, the routing provider an
// aggregator reports, service_tier, ...).
func (c *ChatCompletionResponse) UnmarshalJSON(data []byte) error {
	type alias ChatCompletionResponse
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = ChatCompletionResponse(a)
	c.Extra = decodeExtras(data, responseJSONFields)
	return nil
}

// MarshalJSON re-emits the completion with its unmodelled fields restored.
func (c ChatCompletionResponse) MarshalJSON() ([]byte, error) {
	type alias ChatCompletionResponse
	return encodeWithExtras(alias(c), c.Extra)
}

// UnmarshalJSON decodes usage and retains the accounting fields this package
// does not model, above all the per-request cost aggregators report.
func (u *Usage) UnmarshalJSON(data []byte) error {
	type alias Usage
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*u = Usage(a)
	u.Extra = decodeExtras(data, usageJSONFields)
	return nil
}

// MarshalJSON re-emits usage with its unmodelled fields restored.
func (u Usage) MarshalJSON() ([]byte, error) {
	type alias Usage
	return encodeWithExtras(alias(u), u.Extra)
}
