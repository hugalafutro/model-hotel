package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// StreamTranslator converts an internal OpenAI chat-completion chunk stream into
// the Anthropic Messages SSE event sequence:
//
//	message_start
//	(content_block_start, content_block_delta..., content_block_stop)*  // one group per block
//	message_delta   // stop_reason + cumulative output usage
//	message_stop
//
// It is a stateful state machine keyed on the active content block (blockKind),
// not a text-vs-tool binary, so a thinking-block case is an additive change
// later (plan Gap 5 design constraint). It is single-goroutine: the proxy drives
// it from one streaming loop, exactly where the OpenAI chunks are parsed today.
type StreamTranslator struct {
	messageID string
	model     string

	started bool // message_start emitted

	// Active block state.
	curKind  blockKind
	curIndex int // index of the block currently open
	openMax  int // highest block index opened so far (-1 = none)

	// Tool-call bookkeeping: OpenAI streams tool_calls under their own Index;
	// map that to the Anthropic content-block index we assigned it.
	toolBlockByOAIndex map[int]int

	// Best-effort usage + terminal reason.
	completionTokens int
	finishReason     string // last OpenAI finish_reason observed
	finished         bool   // Finish() already emitted
}

// NewStreamTranslator builds a translator for one response. messageID is the
// Anthropic message id surfaced to the client (e.g. "msg_..."); model is echoed
// back in message_start.
func NewStreamTranslator(messageID, model string) *StreamTranslator {
	return &StreamTranslator{
		messageID:          messageID,
		model:              model,
		curKind:            blockNone,
		openMax:            -1,
		toolBlockByOAIndex: map[int]int{},
	}
}

// writeEvent appends one framed SSE event ("event: <type>\ndata: <json>\n\n").
func writeEvent(buf *bytes.Buffer, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("anthropic: marshal %s event: %w", eventType, err)
	}
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return nil
}

// ensureStarted lazily emits message_start (and a ping, mirroring the real API)
// the first time any content is processed. input_tokens is reported as 0:
// OpenAI streaming does not reveal the prompt count until the terminal usage
// chunk, and the locked decision is best-effort usage with no upstream mutation.
func (t *StreamTranslator) ensureStarted(buf *bytes.Buffer) error {
	if t.started {
		return nil
	}
	t.started = true
	start := messageStartEvent{
		Type: "message_start",
		Message: message{
			ID:           t.messageID,
			Type:         "message",
			Role:         "assistant",
			Model:        t.model,
			Content:      []contentBlock{},
			StopReason:   nil,
			StopSequence: nil,
			Usage:        usage{InputTokens: 0, OutputTokens: 0},
		},
	}
	if err := writeEvent(buf, "message_start", start); err != nil {
		return err
	}
	return writeEvent(buf, "ping", pingEvent{Type: "ping"})
}

// closeOpenBlock emits content_block_stop for the active block, if any.
func (t *StreamTranslator) closeOpenBlock(buf *bytes.Buffer) error {
	if t.curKind == blockNone {
		return nil
	}
	if err := writeEvent(buf, "content_block_stop", contentBlockStopEvent{
		Type:  "content_block_stop",
		Index: t.curIndex,
	}); err != nil {
		return err
	}
	t.curKind = blockNone
	return nil
}

// openTextBlock starts a new text content block, closing any open block first.
func (t *StreamTranslator) openTextBlock(buf *bytes.Buffer) error {
	if t.curKind == blockText {
		return nil
	}
	if err := t.closeOpenBlock(buf); err != nil {
		return err
	}
	t.openMax++
	t.curIndex = t.openMax
	t.curKind = blockText
	return writeEvent(buf, "content_block_start", contentBlockStartEvent{
		Type:         "content_block_start",
		Index:        t.curIndex,
		ContentBlock: contentBlock{Type: "text", Text: ""},
	})
}

// openToolBlock starts a new tool_use content block for the given OpenAI
// tool-call index, closing any open block first. id/name come from the first
// fragment OpenAI streams for that index.
func (t *StreamTranslator) openToolBlock(buf *bytes.Buffer, oaIndex int, id, name string) error {
	if err := t.closeOpenBlock(buf); err != nil {
		return err
	}
	t.openMax++
	t.curIndex = t.openMax
	t.curKind = blockToolUse
	t.toolBlockByOAIndex[oaIndex] = t.curIndex
	if id == "" {
		// Anthropic requires a tool_use id; synthesize a stable one if the
		// upstream omitted it.
		id = fmt.Sprintf("toolu_%s_%d", t.messageID, t.curIndex)
	}
	return writeEvent(buf, "content_block_start", contentBlockStartEvent{
		Type:  "content_block_start",
		Index: t.curIndex,
		ContentBlock: contentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: json.RawMessage("{}"),
		},
	})
}

// Translate processes one OpenAI chunk and returns the SSE bytes to forward to
// the client (possibly empty). It records finish_reason and usage for the
// terminal Finish() events. It never emits message_delta/message_stop itself.
func (t *StreamTranslator) Translate(chunk OAStreamChunk) ([]byte, error) {
	var buf bytes.Buffer

	if chunk.Usage != nil && chunk.Usage.CompletionTokens > 0 {
		t.completionTokens = chunk.Usage.CompletionTokens
	}

	if len(chunk.Choices) == 0 {
		// Usage-only or empty chunk: nothing to forward, state already updated.
		return buf.Bytes(), nil
	}
	choice := chunk.Choices[0]

	if choice.FinishReason != nil && *choice.FinishReason != "" {
		t.finishReason = *choice.FinishReason
	}

	// Text content delta.
	if choice.Delta.Content != "" {
		if err := t.ensureStarted(&buf); err != nil {
			return nil, err
		}
		if err := t.openTextBlock(&buf); err != nil {
			return nil, err
		}
		if err := writeEvent(&buf, "content_block_delta", contentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: t.curIndex,
			Delta: contentDelta{Type: "text_delta", Text: choice.Delta.Content},
		}); err != nil {
			return nil, err
		}
	}

	// Tool-call deltas.
	for _, tc := range choice.Delta.ToolCalls {
		if err := t.ensureStarted(&buf); err != nil {
			return nil, err
		}
		blockIdx, open := t.toolBlockByOAIndex[tc.Index]
		if !open {
			// First fragment for this tool call: open the block (carries id/name).
			if err := t.openToolBlock(&buf, tc.Index, tc.ID, tc.Function.Name); err != nil {
				return nil, err
			}
			blockIdx = t.curIndex
		}
		// Argument fragments stream as input_json_delta partial JSON.
		if tc.Function.Arguments != "" {
			if err := writeEvent(&buf, "content_block_delta", contentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: blockIdx,
				Delta: contentDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
			}); err != nil {
				return nil, err
			}
		}
	}

	return buf.Bytes(), nil
}

// Finish emits the terminal events: it closes any open content block, then
// message_delta (stop_reason + cumulative output_tokens) and message_stop. It
// is idempotent and lazily emits message_start first if no chunk ever did (e.g.
// an empty completion), so the client always sees a well-formed stream.
func (t *StreamTranslator) Finish() ([]byte, error) {
	var buf bytes.Buffer
	if t.finished {
		return buf.Bytes(), nil
	}
	t.finished = true

	if err := t.ensureStarted(&buf); err != nil {
		return nil, err
	}
	if err := t.closeOpenBlock(&buf); err != nil {
		return nil, err
	}

	stop := mapStopReason(t.finishReason)
	if err := writeEvent(&buf, "message_delta", messageDeltaEvent{
		Type:  "message_delta",
		Delta: messageDeltaBody{StopReason: &stop, StopSequence: nil},
		Usage: messageDeltaUsage{OutputTokens: t.completionTokens},
	}); err != nil {
		return nil, err
	}
	if err := writeEvent(&buf, "message_stop", messageStopEvent{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
