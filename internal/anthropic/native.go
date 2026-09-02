package anthropic

import (
	"encoding/json"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// RewriteModel rewrites the top-level "model" field of an Anthropic Messages
// request body to the resolved upstream model id, leaving every other field
// intact. On any parse failure the original body is returned unchanged.
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

// ResponseUsage is the metering summary of one Anthropic Messages response.
// PromptTokens is the whole prompt. CacheHit and CacheMiss split it by how the
// tokens were billed and sum back to it, but only when the response reports a
// cache read; an uncached response leaves both zero.
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
func ParseResponseUsage(body []byte) ResponseUsage {
	var resp struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return ResponseUsage{}
	}
	u, ok := ReadUsage(resp.Usage)
	if !ok {
		return ResponseUsage{}
	}
	return u.summary()
}

// UsageBlock is the usage block an Anthropic-Messages upstream reports, as
// this gateway reads it. The cache fields are absent from responses that use
// no cache, which decodes to zero. Shared with the egress translator, which
// reads the same block. Distinct from the unexported usage in events.go, which
// is the block this gateway WRITES when it emits Anthropic events.
type UsageBlock struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// summary splits the block into the counts the proxy meters. The whole prompt
// is the three disjoint input counts summed; of those, only
// cache_read_input_tokens was served from cache and billed at the cache-hit
// rate. Cache CREATION is a miss: those tokens are processed (and surcharged)
// on this request and only pay off on the next one.
//
// The split is reported only when a cache READ occurred. A response that
// created a cache entry without reading one, or that used no cache at all,
// reports NO cache counts rather than "miss = the whole prompt". Creation
// tokens still count inside PromptTokens, which is what the request is metered
// and priced on.
func (u UsageBlock) summary() ResponseUsage {
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
// an answer, and must not count as the model answering.
//
// The blocks are left undecoded: a block of any type is the model producing
// something.
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
// tool input across a non-streaming Messages response's content blocks, the
// delivered output a usage estimate works from when the response carries no
// usage block.
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

// StreamEvent is the decoded summary of a single Anthropic stream event: the
// event Type, any token usage, and the error message on an "error" event.
type StreamEvent struct {
	Type            string
	InputTokens     int
	CacheHitTokens  int // the cache-served share of InputTokens; 0 when uncached
	CacheMissTokens int // the rest of InputTokens; 0 when uncached, not the whole prompt
	HasInput        bool
	OutputTokens    int
	HasOutput       bool
	ErrorMessage    string // set only when Type == "error"
	// CarriesError reports a populated error member on ANY event type: a
	// relay wraps its rejection as {"type":"error","error":{...}}, sends it
	// bare as {"error":{...}}, or stamps it on an ordinary event, and all of
	// those are error text for the credential mask.
	CarriesError bool
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
// prompt exactly as the non-streaming path does.
func InspectStreamEvent(payload []byte) StreamEvent {
	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		// json.RawMessage for the same reason the error member below is one: a
		// count the provider spells differently (quoted, or with a fraction on
		// it) must not fail the WHOLE event unmarshal and cost the event its
		// type along with its counts.
		Usage json.RawMessage `json:"usage"`
		// json.RawMessage, not a typed object: a relay is free to send a bare
		// string here, and a typed field fails the WHOLE event unmarshal, which
		// costs the event its type, leaves the error uncounted and forwards the
		// frame with no key-shape masking. util.ValueCarries is the same
		// rule the OpenAI-compatible path reads this member with.
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
	info := StreamEvent{Type: ev.Type, CarriesError: util.ValueCarries(ev.Error)}
	switch ev.Type {
	case "message_start":
		var raw json.RawMessage
		if ev.Message != nil {
			raw = ev.Message.Usage
		}
		if usage, ok := ReadUsage(raw); ok {
			u := usage.summary()
			// HasInput means there is a READING, so it follows the figure rather
			// than the event: ReadUsage drops a prompt figure whose addend
			// it could not read, and claiming a reading of zero for that would
			// have the caller overwrite a good count with nothing.
			info.InputTokens, info.HasInput = u.PromptTokens, u.PromptTokens > 0
			info.CacheHitTokens, info.CacheMissTokens = u.CacheHitTokens, u.CacheMissTokens
			if usage.OutputTokens > 0 {
				info.OutputTokens, info.HasOutput = usage.OutputTokens, true
			}
		}
	case "message_delta":
		if usage, ok := ReadUsage(ev.Usage); ok {
			// An output figure that is not positive is not a reading: assigned
			// verbatim, a negative output_tokens reaches the completion
			// metering and draws the key's usage down.
			if usage.OutputTokens > 0 {
				info.OutputTokens, info.HasOutput = usage.OutputTokens, true
			}
		}
	case "error":
		if util.ValueCarries(ev.Error) {
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

// promptAddends are the members Anthropic's prompt figure is SUMMED from.
var promptAddends = []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"}

// ReadUsage decodes an Anthropic usage block on its own, reporting whether
// there was one to read: an absent or null member is not a reading. Decoded
// separately from the event or response around it so a count spelled
// differently (quoted, or with a fraction on it) costs the counts and never
// the event's type or the answer beside it.
//
// Per FIGURE, not per block: a member that could not be read costs the figures
// it FEEDS. output_tokens stands or falls alone; the prompt figure needs all
// three of its addends, since a cache-read count of 20000 lost to an
// unreadable sibling would bill 4, and no estimate replaces a non-zero figure.
func ReadUsage(raw json.RawMessage) (UsageBlock, bool) {
	if !util.JSONMemberSet(raw) {
		return UsageBlock{}, false
	}
	var u UsageBlock
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return UsageBlock{}, false
	}
	if len(util.UnreadableCounts(raw, promptAddends...)) > 0 {
		u.InputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens = 0, 0, 0
	}
	if len(util.UnreadableCounts(raw, "output_tokens")) > 0 {
		u.OutputTokens = 0
	}
	return u, true
}
