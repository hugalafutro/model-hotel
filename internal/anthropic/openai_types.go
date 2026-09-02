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
	Index    int             `json:"index"`
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
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// mapStopReason maps an OpenAI finish_reason to an Anthropic stop_reason.
// Anthropic's vocabulary: end_turn, max_tokens, stop_sequence, tool_use.
func mapStopReason(openaiFinish string) string {
	switch openaiFinish {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop", "content_filter", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}
