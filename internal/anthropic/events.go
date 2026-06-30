// Package anthropic translates between the Anthropic Messages API wire format
// and the OpenAI chat-completions format the proxy pipeline speaks internally.
//
// Design note (2026-06-30): the plan's locked decision #5 said to emit the SSE
// stream using anthropics/anthropic-sdk-go's struct definitions directly. In
// SDK v1.53 those response/event types are decode-oriented: they carry no
// `omitempty`, so marshaling them emits a wall of zero-value junk (a phantom
// `stop_details.type:"refusal"` on every message_start, a full citation/document
// union on every text content_block_start, etc.) that no Anthropic client should
// be handed. We therefore emit via the small, omitempty-clean structs below and
// VALIDATE that output by decoding it back through the real SDK's ssestream
// decoder in the golden tests. The SDK still certifies our wire format; we just
// don't let its decode structs author it.
package anthropic

import "encoding/json"

// blockKind enumerates the content-block shapes the streaming translator can be
// in the middle of emitting. The translator dispatches on this rather than a
// text-vs-tool boolean so reasoning ("thinking") blocks slot in as one more
// case later without reshaping the state machine (plan Gap 5 design constraint).
type blockKind int

const (
	blockNone blockKind = iota
	blockText
	blockToolUse
	blockThinking // reserved; not emitted on the translated path in v1
)

// --- Content blocks (used inside message_start.content and content_block_start) ---

// contentBlock is the union shape carried by content_block_start.content_block
// and assembled into the non-streaming response content array. Only the fields
// relevant to the active block kind are populated; omitempty keeps the rest off
// the wire.
type contentBlock struct {
	Type string `json:"type"`
	// text block
	Text string `json:"text,omitempty"`
	// tool_use block
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// thinking block (reserved)
	Thinking string `json:"thinking,omitempty"`
}

// --- Top-level message object (message_start + non-streaming response) ---

// usage carries Anthropic token accounting. output_tokens is always present
// (Anthropic emits 0 on message_start); input_tokens is present on message_start.
type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// message is the Anthropic Message object. stop_reason / stop_sequence are
// pointers so they serialize as JSON null (not "") before the turn completes,
// matching the real API.
type message struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // always "message"
	Role         string         `json:"role"` // always "assistant"
	Model        string         `json:"model"`
	Content      []contentBlock `json:"content"`
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        usage          `json:"usage"`
}

// --- SSE event envelopes ---
//
// Each is marshaled to the `data:` payload of an `event: <type>` SSE frame by
// writeEvent. They mirror the Anthropic streaming event shapes exactly.

type messageStartEvent struct {
	Type    string  `json:"type"` // "message_start"
	Message message `json:"message"`
}

type contentBlockStartEvent struct {
	Type         string       `json:"type"` // "content_block_start"
	Index        int          `json:"index"`
	ContentBlock contentBlock `json:"content_block"`
}

// contentDelta is the delta union for content_block_delta. text_delta carries
// Text; input_json_delta carries PartialJSON; thinking_delta carries Thinking.
type contentDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
}

type contentBlockDeltaEvent struct {
	Type  string       `json:"type"` // "content_block_delta"
	Index int          `json:"index"`
	Delta contentDelta `json:"delta"`
}

type contentBlockStopEvent struct {
	Type  string `json:"type"` // "content_block_stop"
	Index int    `json:"index"`
}

// messageDeltaBody carries the terminal stop_reason/stop_sequence.
type messageDeltaBody struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// messageDeltaUsage is the cumulative output-token count reported on
// message_delta. Best-effort: 0 when the upstream provider volunteered nothing.
type messageDeltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

type messageDeltaEvent struct {
	Type  string            `json:"type"` // "message_delta"
	Delta messageDeltaBody  `json:"delta"`
	Usage messageDeltaUsage `json:"usage"`
}

type messageStopEvent struct {
	Type string `json:"type"` // "message_stop"
}

type pingEvent struct {
	Type string `json:"type"` // "ping"
}

// errorEvent is the Anthropic streaming + non-streaming error envelope:
//
//	{"type":"error","error":{"type":"...","message":"..."}}
type errorEvent struct {
	Type  string       `json:"type"` // "error"
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
