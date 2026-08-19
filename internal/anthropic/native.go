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

// ResponseUsage is the metering summary of one Anthropic Messages response,
// produced from a single parse. PromptTokens is the whole prompt; CacheHit and
// CacheMiss split it by how the tokens were billed, and always sum back to it.
type ResponseUsage struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
	CacheMissTokens  int
}

// ParseResponseUsage extracts the token counts from a non-streaming Anthropic
// Messages response (top-level usage{...}) for metering. A missing or
// unparseable usage block yields a zero ResponseUsage.
//
// PromptTokens is the SUM of input_tokens, cache_read_input_tokens and
// cache_creation_input_tokens: Anthropic's three counts are disjoint additions,
// not a breakdown, so a cache hit reports input_tokens: 4 alongside
// cache_read_input_tokens: 20000 for a ~20004-token prompt. Metering the bare
// input_tokens under-reports a warm-cache request by the whole cached figure.
// The egress adapter does the same arithmetic (see
// internal/anthropicegress.translateUsage), so both Anthropic paths agree.
func ParseResponseUsage(body []byte) ResponseUsage {
	var resp struct {
		Usage antUsage `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return ResponseUsage{}
	}
	return resp.Usage.summary()
}

// antUsage is an Anthropic usage block. The cache fields are absent from
// responses that use no cache, which decodes to zero and leaves the whole
// prompt counted as a miss.
type antUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// summary splits the block into the counts the proxy meters. The whole prompt
// is the three disjoint input counts summed; of those, only
// cache_read_input_tokens was served from cache and billed at the cache-hit
// rate. Cache CREATION is a miss: those tokens are processed (and surcharged)
// on this request and only pay off on the next one. Reporting the sum without
// this split prices every cached token at full input rate, which is a different
// skew from under-counting, not an absence of one.
func (u antUsage) summary() ResponseUsage {
	return ResponseUsage{
		PromptTokens:     u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
		CompletionTokens: u.OutputTokens,
		CacheHitTokens:   u.CacheReadInputTokens,
		CacheMissTokens:  u.InputTokens + u.CacheCreationInputTokens,
	}
}

// ResponseCarriesContent reports whether a non-streaming Messages response holds
// any content block. A 200 with an empty content array is a status rather than
// an answer, and the proxy's model-retirement path needs the difference: an
// empty completion must not count as the model answering.
//
// The blocks are left undecoded — a block of any type is the model producing
// something, and their vocabulary is Anthropic's to extend.
func ResponseCarriesContent(body []byte) bool {
	var resp struct {
		Content []json.RawMessage `json:"content"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return false
	}
	return len(resp.Content) > 0
}

// StreamEvent is the decoded summary of a single Anthropic stream event,
// produced by InspectStreamEvent for the native passthrough path. It carries
// everything that path needs from one parse: the event Type (so the terminal
// message_stop can be detected and completion gated on it), any token usage, and
// the error message on an "error" event (so a provider-sent error is recorded,
// not just forwarded blind).
type StreamEvent struct {
	Type            string
	InputTokens     int
	CacheHitTokens  int // the cache-served share of InputTokens
	CacheMissTokens int // the rest of InputTokens, billed at full input rate
	HasInput        bool
	OutputTokens    int
	HasOutput       bool
	ErrorMessage    string // set only when Type == "error"
}

// InspectStreamEvent decodes one Anthropic stream event payload (the JSON after
// "data: "). message_start carries the input usage; message_delta carries the
// cumulative usage.output_tokens; an "error" event carries error.message. A
// payload that does not parse yields a zero StreamEvent (Type == "").
//
// InputTokens and its cache split come from the same arithmetic
// ParseResponseUsage does, so a streamed warm-cache request meters its full
// prompt — and prices it — exactly as the non-streaming path would.
func InspectStreamEvent(payload []byte) StreamEvent {
	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Usage *antUsage `json:"usage"`
		} `json:"message"`
		Usage *antUsage `json:"usage"`
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
			u := ev.Message.Usage.summary()
			info.InputTokens, info.HasInput = u.PromptTokens, true
			info.CacheHitTokens, info.CacheMissTokens = u.CacheHitTokens, u.CacheMissTokens
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
