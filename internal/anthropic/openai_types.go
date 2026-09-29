package anthropic

import (
	"encoding/json"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// This file defines the minimal subset of the OpenAI chat-completions wire
// format the translators consume. The proxy adapts its parsed chunks into
// these on the way in.

// OAStreamChunk is one OpenAI `chat.completion.chunk` SSE payload.
type OAStreamChunk struct {
	Choices []OAStreamChoice `json:"choices"`
	Usage   *OAUsage         `json:"usage"`
}

// OAStreamChoice is one choice within a streaming chunk.
type OAStreamChoice struct {
	Delta        OAStreamDelta `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

// OAStreamDelta is the incremental delta on a streaming choice.
type OAStreamDelta struct {
	Content   string            `json:"content"`
	ToolCalls []OAToolCallDelta `json:"tool_calls"`
}

// OAToolCallDelta is one incremental tool-call fragment. OpenAI streams the
// function name (and id) on the first fragment for a given Index, then streams
// the JSON arguments as a string in successive fragments under the same Index.
type OAToolCallDelta struct {
	// Index is nil when the upstream omitted it. Some OpenAI-compatible servers
	// stream tool calls without one; the translator then keys the call by its
	// id, and a fragment carrying neither continues the call last opened.
	Index    *int            `json:"index"`
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function OAFunctionDelta `json:"function"`
	// ExtraContent carries the Gemini 3 thought signature, on the fragment
	// that opens the call; raw, read leniently.
	ExtraContent json.RawMessage `json:"extra_content"`
}

// OAFunctionDelta carries the function name and a fragment of the arguments
// JSON string.
type OAFunctionDelta struct {
	Name string `json:"name"`
	// util.ToolArguments accepts both the string and the object form: a
	// plain string drops an object-form tool call from the translated
	// stream, leaving a stop_reason of "tool_use" with no tool_use block,
	// a shape the Anthropic SDKs reject.
	Arguments util.ToolArguments `json:"arguments"`
}

// OAUsage is the OpenAI usage block. Only the token counts matter for the
// best-effort Anthropic usage mapping.
type OAUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// PromptCacheHitTokens is DeepSeek's top-level spelling of the cached
	// share of prompt_tokens.
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
}

// cachedTokens is the cached share of the prompt, in whichever spelling the
// upstream used: DeepSeek's prompt_cache_hit_tokens first (as the proxy's
// metering reads it), else OpenAI's prompt_tokens_details.cached_tokens.
func (u OAUsage) cachedTokens() int {
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	return u.PromptTokensDetails.CachedTokens
}

// splitPrompt divides an OpenAI prompt count the Anthropic way: OpenAI's
// prompt_tokens includes the cached_tokens served from cache, while Anthropic
// reports those as cache_read_input_tokens and input_tokens as the rest, so
// the two always sum to prompt_tokens. A cached figure above the prompt count
// cannot be a share of it and is dropped.
func splitPrompt(prompt, cached int) (input, cacheRead int) {
	if cached <= 0 || cached > prompt {
		return prompt, 0
	}
	return prompt - cached, cached
}

// mapStopReason maps an OpenAI finish_reason to an Anthropic stop_reason.
// Anthropic's vocabulary: end_turn, max_tokens, stop_sequence, tool_use.
func mapStopReason(openaiFinish string) string {
	switch openaiFinish {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		// stop, content_filter, an absent reason and anything newer: the turn
		// simply ended.
		return "end_turn"
	}
}
