package anthropic

import "encoding/json"

// RewriteModel rewrites the top-level "model" field of an Anthropic Messages
// request body to the resolved upstream model id, leaving every other field
// (system, messages, tools, cache_control, thinking config, ...) semantically
// intact. The round-trip through a map may reorder top-level keys, but JSON
// object key order is not significant. This is the only mutation the native
// passthrough path makes: the proxy routes on "provider/model" or "hotel/group",
// but the upstream Anthropic API must receive the bare model id. On any parse
// failure the original body is returned unchanged (the upstream surfaces a clear
// model error).
func RewriteModel(body []byte, model string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	mb, err := json.Marshal(model)
	if err != nil {
		return body
	}
	m["model"] = mb
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// ParseResponseUsage extracts input/output token counts from a non-streaming
// Anthropic Messages response (top-level usage{input_tokens, output_tokens}) for
// metering. Missing/unparseable usage yields zeros.
func ParseResponseUsage(body []byte) (inputTokens, outputTokens int) {
	var resp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return 0, 0
	}
	return resp.Usage.InputTokens, resp.Usage.OutputTokens
}

// StreamEvent is the decoded summary of a single Anthropic stream event,
// produced by InspectStreamEvent for the native passthrough path. It carries
// everything that path needs from one parse: the event Type (so the terminal
// message_stop can be detected and completion gated on it), any token usage, and
// the error message on an "error" event (so a provider-sent error is recorded,
// not just forwarded blind).
type StreamEvent struct {
	Type         string
	InputTokens  int
	HasInput     bool
	OutputTokens int
	HasOutput    bool
	ErrorMessage string // set only when Type == "error"
}

// InspectStreamEvent decodes one Anthropic stream event payload (the JSON after
// "data: "). message_start carries usage.input_tokens; message_delta carries the
// cumulative usage.output_tokens; an "error" event carries error.message. A
// payload that does not parse yields a zero StreamEvent (Type == "").
func InspectStreamEvent(payload []byte) StreamEvent {
	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &ev) != nil {
		return StreamEvent{}
	}
	info := StreamEvent{Type: ev.Type}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			info.InputTokens, info.HasInput = ev.Message.Usage.InputTokens, true
			if ev.Message.Usage.OutputTokens > 0 {
				info.OutputTokens, info.HasOutput = ev.Message.Usage.OutputTokens, true
			}
		}
	case "message_delta":
		if ev.Usage != nil {
			info.OutputTokens, info.HasOutput = ev.Usage.OutputTokens, true
		}
	case "error":
		if ev.Error != nil {
			info.ErrorMessage = ev.Error.Message
		}
	}
	return info
}
