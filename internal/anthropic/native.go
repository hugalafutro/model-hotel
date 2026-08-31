package anthropic

import (
	"encoding/json"

	"github.com/hugalafutro/model-hotel/internal/util"
)

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
// produced from a single parse. PromptTokens is the whole prompt. CacheHit and
// CacheMiss split it by how the tokens were billed and sum back to it, but only
// when the response reports cache activity at all; an uncached response leaves
// both zero rather than calling the whole prompt a miss (see summary).
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
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil || !util.JSONMemberSet(resp.Usage) {
		return ResponseUsage{}
	}
	return readUsage(resp.Usage)
}

// antUsage is an Anthropic usage block. The cache fields are absent from
// responses that use no cache, which decodes to zero.
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
//
// The split is reported only when a cache READ occurred. A response that
// created a cache entry without reading one — and one with no cache activity at
// all — reports NO cache counts rather than "miss = the whole prompt". The
// translated egress path cannot express either reading: extractCacheTokens
// keys off the cache-READ fields alone, so it yields (0, 0) for both, and one
// Anthropic path claiming cache data the other cannot is exactly the
// inconsistency this split exists to remove. It is also what every other
// provider reports for a request that read nothing from cache, which is what
// the dashboard's cache panel and the cache-miss stats series assume.
//
// Creation tokens are never lost either way: they count inside PromptTokens,
// which is what the request is metered and priced on.
func (u antUsage) summary() ResponseUsage {
	out := ResponseUsage{
		PromptTokens:     u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
		CompletionTokens: u.OutputTokens,
	}
	if u.CacheReadInputTokens > 0 {
		out.CacheHitTokens = u.CacheReadInputTokens
		out.CacheMissTokens = u.InputTokens + u.CacheCreationInputTokens
	}
	return out
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

// ResponseTextBytes is the byte length of the text, thinking, tool name and
// tool input of a non-streaming Messages response's content blocks, the delivered output a
// usage estimate works from when the response carries no usage block.
func ResponseTextBytes(body []byte) int {
	var resp struct {
		Content []struct {
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return 0
	}
	n := 0
	for _, block := range resp.Content {
		n += len(block.Text) + len(block.Thinking) + len(block.Name) + len(block.Input)
	}
	return n
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
	CacheHitTokens  int // the cache-served share of InputTokens; 0 when uncached
	CacheMissTokens int // the rest of InputTokens; 0 when uncached, not the whole prompt
	HasInput        bool
	OutputTokens    int
	HasOutput       bool
	ErrorMessage    string // set only when Type == "error"
	// TextBytes is the byte length of the output a content block event carries:
	// a content_block_delta's text, thinking or partial JSON, and a
	// content_block_start's tool name (its input starts empty and arrives as
	// deltas, so nothing is counted twice). It is the delivered output the
	// passthrough estimates from when the stream ends before message_delta
	// reports output_tokens.
	TextBytes int
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
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		// json.RawMessage for the same reason the error member below is one: a
		// count the provider spelled differently — quoted, or with a fraction on
		// it — failed the WHOLE event unmarshal, so the event lost its TYPE with
		// its counts and message_stop and the error events stopped being
		// recognised for that frame.
		Usage json.RawMessage `json:"usage"`
		// json.RawMessage, not a typed object: a relay is free to send a bare
		// string here, and a typed field failed the WHOLE event unmarshal — so
		// the event lost its type too, the error went uncounted, and the frame
		// was forwarded to the caller with no key-shape masking at all,
		// credential included. util.ErrorMemberCarries is the same rule the
		// OpenAI-compatible path reads this member with.
		Error json.RawMessage `json:"error"`
		Delta *struct {
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
		ContentBlock *struct {
			Name string `json:"name"`
		} `json:"content_block"`
	}
	if json.Unmarshal(payload, &ev) != nil {
		return StreamEvent{}
	}
	info := StreamEvent{Type: ev.Type}
	switch ev.Type {
	case "message_start":
		var raw json.RawMessage
		if ev.Message != nil {
			raw = ev.Message.Usage
		}
		if usage, ok := readEventUsage(raw); ok {
			u := usage.summary()
			// HasInput means there is a READING, so it follows the figure rather
			// than the event: readEventUsage drops a prompt figure whose addend
			// it could not read, and claiming a reading of zero for that would
			// have the caller overwrite a good count with nothing.
			info.InputTokens, info.HasInput = u.PromptTokens, u.PromptTokens > 0
			info.CacheHitTokens, info.CacheMissTokens = u.CacheHitTokens, u.CacheMissTokens
			if usage.OutputTokens > 0 {
				info.OutputTokens, info.HasOutput = usage.OutputTokens, true
			}
		}
	case "message_delta":
		if usage, ok := readEventUsage(ev.Usage); ok {
			// The guard message_start has always had: an output figure that is
			// not positive is not a reading. Assigned verbatim, a negative
			// output_tokens here reached the completion metering after the
			// whole answer had been forwarded and drew the key's usage down.
			if usage.OutputTokens > 0 {
				info.OutputTokens, info.HasOutput = usage.OutputTokens, true
			}
		}
	case "error":
		if util.ErrorMemberCarries(ev.Error) {
			info.ErrorMessage = util.ErrorMemberMessage(ev.Error)
		}
	case "content_block_delta":
		if ev.Delta != nil {
			info.TextBytes = len(ev.Delta.Text) + len(ev.Delta.Thinking) + len(ev.Delta.PartialJSON)
		}
	case "content_block_start":
		if ev.ContentBlock != nil {
			info.TextBytes = len(ev.ContentBlock.Name)
		}
	}
	return info
}

// readEventUsage decodes a stream event's usage member on its own, reporting
// whether there was one to read. A count spelled differently must not cost the
// event its type, and a member that cannot be read at all must not cost the
// counts beside it.
func readEventUsage(raw json.RawMessage) (antUsage, bool) {
	if !util.JSONMemberSet(raw) {
		return antUsage{}, false
	}
	var u antUsage
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return antUsage{}, false
	}
	// A member that could not be read costs the figures it FEEDS, not the whole
	// block. See readUsage.
	if len(util.UnreadableCounts(raw, promptAddends...)) > 0 {
		u.InputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens = 0, 0, 0
	}
	if len(util.UnreadableCounts(raw, "output_tokens")) > 0 {
		u.OutputTokens = 0
	}
	return u, true
}

// promptAddends are the members Anthropic's prompt figure is SUMMED from.
var promptAddends = []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"}

// readUsage decodes an Anthropic usage block per FIGURE, not per block.
//
// Two rules were tried and both were wrong. Keeping whatever decoded corrupted
// the prompt figure, which is input_tokens plus both cache counts: a cache-read
// count of 20000 lost to an unreadable sibling billed 4, and 4 is non-zero, so
// estimateMissingUsage never replaced it. Dropping the whole block instead threw
// away output_tokens — read straight off one member, so never in doubt — and an
// answer with a perfectly good completion count then read as empty, which since
// #812 charges the provider's circuit breaker.
//
// So a member that could not be read costs the figures it FEEDS. output_tokens
// stands or falls alone; the prompt figure needs all three of its addends.
func readUsage(raw json.RawMessage) ResponseUsage {
	if !util.JSONMemberSet(raw) {
		return ResponseUsage{}
	}
	var u antUsage
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return ResponseUsage{}
	}
	if len(util.UnreadableCounts(raw, promptAddends...)) > 0 {
		u.InputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens = 0, 0, 0
	}
	if len(util.UnreadableCounts(raw, "output_tokens")) > 0 {
		u.OutputTokens = 0
	}
	return u.summary()
}
