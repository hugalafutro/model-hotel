package anthropic

import "encoding/json"

// RewriteModel rewrites the top-level "model" field of an Anthropic Messages
// request body to the resolved upstream model id, leaving everything else
// (system, messages, tools, cache_control, thinking config, ...) byte-for-byte
// intact. This is the only mutation the native passthrough path makes: the proxy
// routes on "provider/model" or "hotel/group", but the upstream Anthropic API
// must receive the bare model id. On any parse failure the original body is
// returned unchanged (the upstream will surface a clear model error).
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

// ScanStreamUsage extracts token usage from a single Anthropic stream event
// payload (the JSON after "data: ") for best-effort metering on the native
// passthrough path. message_start carries usage.input_tokens (output 0);
// message_delta carries the cumulative usage.output_tokens. The bool returns
// report which field this event provided so the caller only overwrites real
// values.
func ScanStreamUsage(payload []byte) (inputTokens int, hasInput bool, outputTokens int, hasOutput bool) {
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
	}
	if json.Unmarshal(payload, &ev) != nil {
		return 0, false, 0, false
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			return ev.Message.Usage.InputTokens, true, ev.Message.Usage.OutputTokens, ev.Message.Usage.OutputTokens > 0
		}
	case "message_delta":
		if ev.Usage != nil {
			return 0, false, ev.Usage.OutputTokens, true
		}
	}
	return 0, false, 0, false
}
