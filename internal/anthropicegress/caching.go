package anthropicegress

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// MaxCacheBreakpoints is Anthropic's limit on cache_control markers per request.
// applyCacheBreakpoints never places more than this many.
const MaxCacheBreakpoints = 4

var ephemeral = &antCacheControl{Type: "ephemeral"}

// applyCacheBreakpoints places Anthropic prompt-cache breakpoints on the built
// request. Without one Anthropic caches nothing, so every turn of an agent
// loop re-reads its whole context at full price; with them a prefix that
// repeats is read back at the cache-read price. Breakpoints go where the
// cache hierarchy (tools, then system, then messages) makes a reusable prefix:
//
//   - the last tool, so the tool set is one prefix shared by every request
//     that carries it;
//   - the system prompt, lifted into array form because only a block carries
//     cache_control, shared by every conversation under the same instructions;
//   - the last block of the final turn, which moves forward as a conversation
//     grows (Anthropic looks back up to twenty block positions from a
//     breakpoint for an earlier cache entry, so the previous request's prefix is
//     found again);
//   - the last block of the turn before it, the standing entry a growing
//     conversation hits while the final one is being written.
//
// That is exactly MaxCacheBreakpoints when every part is present and fewer
// otherwise, and a chat-completions client has no way of adding its own, so the
// limit cannot be exceeded. A turn marked here is promoted from string content
// to one text block; unmarked turns keep whatever shape they had. Prompts below
// a model's minimum cacheable length are accepted and simply not cached.
//
// Anthropic rejects a breakpoint on an empty text block, and a text block that
// is only whitespace reads as empty to it. The translator drops empty text
// already, but not whitespace-only text (a client's "\n" turn is forwarded as
// written), so a whitespace-only system prompt is left as a plain string, and
// a turn's breakpoint goes on its last block that is not whitespace-only text,
// or nowhere when it has none.
func applyCacheBreakpoints(out *antRequest) {
	if n := len(out.Tools); n > 0 {
		out.Tools[n-1].CacheControl = ephemeral
	}
	if system, ok := out.System.(string); ok && strings.TrimSpace(system) != "" {
		out.System = []antSystemBlock{{Type: "text", Text: system, CacheControl: ephemeral}}
	}
	for i := len(out.Messages) - 1; i >= 0 && i >= len(out.Messages)-2; i-- {
		blocks := contentBlocks(out.Messages[i].Content)
		if j := lastCacheableBlock(blocks); j >= 0 {
			blocks[j].CacheControl = ephemeral
			out.Messages[i].Content = blocks
		}
	}
}

// lastCacheableBlock is the index of the last block a breakpoint may sit on:
// any block that is not a text block of only whitespace. -1 when there is none.
func lastCacheableBlock(blocks []antBlock) int {
	for j := len(blocks) - 1; j >= 0; j-- {
		if blocks[j].Type != "text" || strings.TrimSpace(blocks[j].Text) != "" {
			return j
		}
	}
	return -1
}

// CacheControlRefused reports a Messages 400 that rejects the cache_control
// field itself: an endpoint that does not know it. Anthropic never refuses the
// field (every active Claude model supports prompt caching), but an
// operator-entered Messages endpoint that is not Anthropic's can, and so could
// a relay in front of it. The proxy learns the refusal per model and rebuilds
// without breakpoints, so such an endpoint costs one wasted round-trip once
// rather than a 400 on every request.
//
// Narrow on purpose, because learning it switches caching off for that model
// until restart, which would silently put every later request back at full
// price. The message has to name the field AND say it is unknown or not
// allowed, the way a schema validator does ("cache_control: Extra inputs are
// not permitted", "unknown field cache_control"). Anthropic's own 400s that
// mention the field are about a particular body, not the field, and are
// excluded by their wording: "cache_control cannot be set for empty text
// blocks", "A maximum of 4 blocks with cache_control", and a thinking block
// carrying one. None of those is a reason to stop caching.
//
// The message is read from the {"error":{"message"}} envelope when there is
// one; otherwise from the other shapes a validator answers in (a top-level
// "message", a FastAPI "detail" string or list of {loc, msg} items), and from
// the body itself only when it is not JSON at all. Never from a JSON body's
// other members: a pydantic item echoes the request under "input", and a
// refusal word in the user's own text must not switch caching off. Each
// validation item is judged on its own, so "cache_control: field required"
// beside "top_p: Extra inputs are not permitted" is not a refusal of the
// field either.
func CacheControlRefused(body []byte) bool {
	for _, text := range refusalTexts(body) {
		if refusesCacheControl(strings.ToLower(text)) {
			return true
		}
	}
	return false
}

// refusesCacheControl is the per-text rule CacheControlRefused describes.
func refusesCacheControl(msg string) bool {
	if !strings.Contains(msg, "cache_control") && !strings.Contains(msg, "cache control") {
		return false
	}
	// Full phrasings, not bare words, so a refusal that happens to mention a
	// maximum or a ttl is still learned.
	for _, bodySpecific := range []string{"empty text", "maximum of", "thinking block", "cache_control.ttl"} {
		if strings.Contains(msg, bodySpecific) {
			return false
		}
	}
	for _, fieldRefusal := range []string{"extra inputs", "not permitted", "unknown", "unrecognized", "unexpected", "not supported", "not allowed", "invalid field", "unsupported"} {
		if strings.Contains(msg, fieldRefusal) {
			return true
		}
	}
	return false
}

// refusalTexts are the parts of a 400 body a refusal can be read from, each
// judged on its own: the error envelope's message, else the validator members
// of a JSON object ("message" when it is a string; "detail" as a string, or
// one text per validation item, its location joined to its message), else the
// raw text of a body that is not a JSON object (bounded; plain text, an array,
// a bare string cannot carry the echoed request this guards against). A JSON
// object of any other shape yields nothing, so nothing is learned from it.
func refusalTexts(body []byte) []string {
	if msg := util.ErrorEnvelopeMessage(body); msg != "" {
		return []string{msg}
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil {
		return []string{string(body[:min(len(body), 4096)])}
	}
	var texts []string
	var message string
	if json.Unmarshal(members["message"], &message) == nil {
		texts = append(texts, message)
	}
	var detail string
	if json.Unmarshal(members["detail"], &detail) == nil {
		texts = append(texts, detail)
	}
	var items []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(members["detail"], &items) == nil {
		for _, item := range items {
			parts := make([]string, 0, len(item.Loc)+1)
			for _, loc := range item.Loc {
				parts = append(parts, fmt.Sprint(loc))
			}
			texts = append(texts, strings.Join(append(parts, item.Msg), " "))
		}
	}
	return texts
}
