package anthropicegress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// --- Incoming Anthropic Messages response shape ---
//
// antBlock (request.go) has no thinking/signature fields and cannot decode a
// response content block, so this package declares its own response-side
// block type.

type antResponse struct {
	Type       string         `json:"type"` // "message" (or "error")
	Content    []antRespBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      *antRespUsage  `json:"usage"`
	Error      *antRespError  `json:"error"`
}

type antRespBlock struct {
	Type string `json:"type"` // "text" | "thinking" | "tool_use" | other (ignored)
	// text
	Text string `json:"text,omitempty"`
	// thinking
	Thinking string `json:"thinking,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// antRespUsage carries Anthropic token accounting, including the two cache
// fields Anthropic reports at the top level of usage (unlike OpenAI, which
// nests its cached-read count under prompt_tokens_details).
type antRespUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// antRespError is the body of an Anthropic {"type":"error"} envelope. Only
// Type is surfaced in error messages — Message can echo fragments of the
// request that produced it, and response content must never reach an error
// string.
type antRespError struct {
	Type string `json:"type"`
}

// --- Outgoing OpenAI chat-completion response shape ---

type completionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []completionChoice `json:"choices"`
	Usage   *completionUsage   `json:"usage,omitempty"`
}

type completionChoice struct {
	Index        int               `json:"index"`
	Message      completionMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type completionMessage struct {
	Role             string               `json:"role"`
	Content          *string              `json:"content"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	ToolCalls        []completionToolCall `json:"tool_calls,omitempty"`
}

type completionToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// completionUsage mirrors the fields internal/proxy.Usage reads off a
// chat-completion response: the standard three, plus the cache pair at the
// top level (Anthropic's own field names, which proxy.Usage decodes
// directly) and the read count restated under prompt_tokens_details for
// providers that only look there.
type completionUsage struct {
	PromptTokens             int                     `json:"prompt_tokens"`
	CompletionTokens         int                     `json:"completion_tokens"`
	TotalTokens              int                     `json:"total_tokens"`
	CacheCreationInputTokens int                     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                     `json:"cache_read_input_tokens,omitempty"`
	PromptTokensDetails      *completionCacheDetails `json:"prompt_tokens_details,omitempty"`
}

type completionCacheDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// BuildChatCompletion converts a non-streaming Anthropic Messages response
// body into an OpenAI chat-completion body. id, model and created are
// supplied by the caller (the model string the client requested is echoed
// back, matching the proxy's existing dialect adapters).
func BuildChatCompletion(anthropicBody []byte, id, model string, created int64) ([]byte, error) {
	var resp antResponse
	if err := json.Unmarshal(anthropicBody, &resp); err != nil {
		return nil, fmt.Errorf("anthropicegress: invalid upstream response: %w", err)
	}
	if resp.Type == "error" {
		kind := "unknown"
		if resp.Error != nil && resp.Error.Type != "" {
			kind = resp.Error.Type
		}
		return nil, fmt.Errorf("anthropicegress: upstream error: %s", kind)
	}

	text, reasoning, toolCalls := translateContent(resp.Content)

	msg := completionMessage{
		Role:             "assistant",
		Content:          &text,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	}

	out := completionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []completionChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: mapFinishReason(resp.StopReason),
		}},
		Usage: translateUsage(resp.Usage),
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("anthropicegress: marshal chat completion: %w", err)
	}
	return encoded, nil
}

// translateContent splits an Anthropic content block list into the three
// pieces a chat-completion message carries separately: visible text, thinking
// text (which OpenAI has no content block for) and tool calls. Block types
// with no chat-completion equivalent (redacted_thinking, server-side tool
// blocks, ...) are dropped rather than forwarded.
func translateContent(blocks []antRespBlock) (text, reasoning string, toolCalls []completionToolCall) {
	var textParts, reasoningParts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "thinking":
			reasoningParts = append(reasoningParts, b.Thinking)
		case "tool_use":
			tc := completionToolCall{ID: b.ID, Type: "function"}
			tc.Function.Name = b.Name
			tc.Function.Arguments = toolArguments(b.Input)
			toolCalls = append(toolCalls, tc)
		}
	}
	return strings.Join(textParts, ""), strings.Join(reasoningParts, ""), toolCalls
}

// toolArguments re-encodes a tool_use block's input object as the compact
// JSON string OpenAI's tool_calls[].function.arguments field expects. Empty
// or malformed input becomes "{}" rather than shipping a broken string.
func toolArguments(input json.RawMessage) string {
	if len(input) == 0 || !json.Valid(input) {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, input); err != nil {
		return "{}"
	}
	return buf.String()
}

// mapFinishReason maps an Anthropic stop_reason to an OpenAI finish_reason.
// An absent or unrecognised reason (including the pause/refusal reasons newer
// Anthropic models can emit) maps to "stop" rather than failing the
// translation over a field that only ever changes client-side behaviour.
func mapFinishReason(stopReason string) string {
	switch stopReason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default: // "end_turn", "stop_sequence", "", and anything unrecognised
		return "stop"
	}
}

// translateUsage maps Anthropic usage onto OpenAI usage. The cache-creation
// and cache-read counts, when present, ride through under their Anthropic
// field names (which proxy.Usage decodes directly) and the read count is
// additionally restated under prompt_tokens_details.cached_tokens for the
// OpenAI-shaped nested field.
func translateUsage(u *antRespUsage) *completionUsage {
	if u == nil {
		return nil
	}
	out := &completionUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
	if u.CacheCreationInputTokens > 0 {
		out.CacheCreationInputTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > 0 {
		out.CacheReadInputTokens = u.CacheReadInputTokens
		out.PromptTokensDetails = &completionCacheDetails{CachedTokens: u.CacheReadInputTokens}
	}
	return out
}
