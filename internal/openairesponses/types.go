// Package openairesponses translates between the OpenAI chat-completions wire
// format the gateway speaks and the OpenAI Responses API (/v1/responses).
//
// Direction is the mirror image of internal/anthropic: there the CLIENT speaks
// a foreign dialect and the upstream speaks chat-completions; here the client
// speaks chat-completions and the UPSTREAM demands the foreign dialect.
// OpenAI's newest models (gpt-5.4+, the gpt-5.6 family) reject tool calling
// combined with reasoning over /v1/chat/completions and require /v1/responses
// (see plans/openai-responses-endpoint.md), so the proxy translates the
// request on the way out and the response/stream back on the way in. Like the
// anthropic package this is a leaf: the proxy composes it, never the reverse.
package openairesponses

import "encoding/json"

// --- Responses API request shape (outgoing) ---

// Request is the subset of the Responses API request the gateway emits.
type Request struct {
	Model             string          `json:"model"`
	Input             []any           `json:"input"`
	Instructions      string          `json:"instructions,omitempty"`
	Tools             []Tool          `json:"tools,omitempty"`
	ToolChoice        any             `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	Reasoning         *Reasoning      `json:"reasoning,omitempty"`
	Text              *TextConfig     `json:"text,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	// Store is always false: MH is a stateless gateway and must not persist
	// conversation state at OpenAI (no omitempty — the explicit false matters).
	Store  bool `json:"store"`
	Stream bool `json:"stream,omitempty"`
}

// Tool is a Responses function tool. Unlike chat-completions the definition is
// flat (internally tagged), not nested under a "function" object.
type Tool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// Reasoning configures reasoning effort and asks for a summary back (the only
// reasoning artifact a stateless gateway can surface to the client).
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// TextConfig carries the structured-output format (chat response_format
// equivalent).
type TextConfig struct {
	Format json.RawMessage `json:"format"`
}

// messageItem is one conversational turn in Request.Input.
type messageItem struct {
	Type    string        `json:"type"` // "message"
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

// contentPart is one typed part inside a message item.
type contentPart struct {
	Type     string `json:"type"` // input_text | output_text | input_image
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// functionCallItem replays a prior assistant tool call from the transcript.
type functionCallItem struct {
	Type      string `json:"type"` // "function_call"
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// functionCallOutputItem replays a prior tool result from the transcript.
type functionCallOutputItem struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// --- Responses API response shape (incoming) ---

// Response is the subset of a Responses API response the gateway reads.
type Response struct {
	ID                string             `json:"id"`
	Model             string             `json:"model"`
	CreatedAt         int64              `json:"created_at"`
	Status            string             `json:"status"` // completed | incomplete | failed | ...
	IncompleteDetails *IncompleteDetails `json:"incomplete_details"`
	Error             *ResponseError     `json:"error"`
	Output            []OutputItem       `json:"output"`
	Usage             *Usage             `json:"usage"`
}

// IncompleteDetails says why a response stopped early.
type IncompleteDetails struct {
	Reason string `json:"reason"` // e.g. "max_output_tokens"
}

// ResponseError is the error payload on a failed response.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OutputItem is one item in Response.Output. A union: message, reasoning and
// function_call items each populate their own fields; decoding the union into
// one struct keeps unknown item types harmlessly empty.
type OutputItem struct {
	Type string `json:"type"` // message | reasoning | function_call | ...
	ID   string `json:"id"`
	Role string `json:"role"`
	// message
	Content []OutputContent `json:"content"`
	// reasoning
	Summary []SummaryPart `json:"summary"`
	// function_call
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OutputContent is one content part of a message output item.
type OutputContent struct {
	Type string `json:"type"` // output_text | refusal | ...
	Text string `json:"text"`
}

// SummaryPart is one part of a reasoning item's summary.
type SummaryPart struct {
	Type string `json:"type"` // summary_text
	Text string `json:"text"`
}

// Usage is the Responses usage block.
type Usage struct {
	InputTokens         int                  `json:"input_tokens"`
	OutputTokens        int                  `json:"output_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details"`
}

// InputTokensDetails breaks down input tokens (prompt cache hits).
type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// OutputTokensDetails breaks down output tokens (reasoning share).
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// --- chat-completions shapes this package emits back to the pipeline ---

// chatResponse is a non-streaming chat.completion object.
type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int             `json:"index"`
	Message      chatRespMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// chatRespMessage is the assistant message of a translated response. Content
// is any so a tool-call-only response can carry the conventional null.
type chatRespMessage struct {
	Role             string         `json:"role"`
	Content          any            `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
}

// chatToolCall mirrors the chat-completions tool_call object. Index is only
// set on streaming deltas.
type chatToolCall struct {
	Index    *int             `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type chatUsage struct {
	PromptTokens            int                          `json:"prompt_tokens"`
	CompletionTokens        int                          `json:"completion_tokens"`
	TotalTokens             int                          `json:"total_tokens"`
	PromptTokensDetails     *chatPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *chatCompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type chatPromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type chatCompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// chatChunk is one chat.completion.chunk streaming payload.
type chatChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []chatChunkChoice `json:"choices"`
	Usage   *chatUsage        `json:"usage,omitempty"`
}

type chatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type chatDelta struct {
	Role             string         `json:"role,omitempty"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
}

// mapStatusFinishReason maps a terminal Responses status to the chat
// finish_reason vocabulary. hasToolCalls wins over everything: a turn that
// produced function calls finishes "tool_calls" regardless of status.
func mapStatusFinishReason(status string, details *IncompleteDetails, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	if status == "incomplete" && details != nil && details.Reason == "max_output_tokens" {
		return "length"
	}
	return "stop"
}
