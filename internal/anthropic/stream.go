package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// StreamTranslator converts an internal OpenAI chat-completion chunk stream into
// the Anthropic Messages SSE event sequence:
//
//	message_start
//	(content_block_start, content_block_delta..., content_block_stop)*  // one group per block
//	message_delta   // stop_reason + cumulative token usage
//	message_stop
//
// It is a state machine keyed on the active content block (blockKind), not a
// text-vs-tool binary. It is single-goroutine: the proxy drives it from one
// streaming loop.
type StreamTranslator struct {
	messageID string
	model     string

	started bool // message_start emitted

	// Active block state.
	curKind  blockKind
	curIndex int // index of the block currently open
	openMax  int // highest block index opened so far (-1 = none)

	// Tool-call bookkeeping: OpenAI streams tool_calls under their own Index;
	// map that to the Anthropic content-block index we assigned it. A fragment
	// without an index is keyed by its call id (a synthetic negative index per
	// id), and one with neither continues lastToolOAIndex, the call streamed
	// last, since fragments of one call arrive contiguously.
	toolBlockByOAIndex map[int]int
	idxByCallID        map[string]int
	idByIndex          map[int]string // wire index -> the id that last opened under it
	aliasOf            map[int]int    // wire index -> the call it currently names
	lastToolOAIndex    int

	// Best-effort usage + terminal reason.
	promptTokens     int
	cachedTokens     int // prompt_tokens_details.cached_tokens, a share of promptTokens
	completionTokens int
	finishReason     string // last OpenAI finish_reason observed
	finished         bool   // Finish() already emitted

	// lateSignatures counts thought signatures that arrive on a fragment
	// after the one that opened the call's block, where the id (their only
	// carrier) is already fixed. The loss surfaces a turn later as Gemini's
	// refusal, so the proxy logs the count at the end of the stream.
	lateSignatures int
}

// LateSignatures reports how many thought signatures the stream could not
// carry because they arrived after their call's block had opened.
func (t *StreamTranslator) LateSignatures() int { return t.lateSignatures }

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
		idxByCallID:        map[string]int{},
		idByIndex:          map[int]string{},
		aliasOf:            map[int]int{},
	}
}

// oaIndexFor resolves the OpenAI index a tool-call fragment belongs to, see
// toolBlockByOAIndex. Mixed shapes are read for what they mean: an opener
// that reuses an index another call already holds (a provider that stamps 0
// on every call) is a new call, and that wire index then names the new call
// for the id-less fragments that follow (aliasOf, latest opener wins); a
// continuation whose index opened nothing while the last call was id-keyed
// belongs to that call.
func (t *StreamTranslator) oaIndexFor(tc OAToolCallDelta) int {
	switch {
	case tc.Index != nil:
		wire := *tc.Index
		idx := wire
		if tc.ID != "" {
			if known, ok := t.idxByCallID[tc.ID]; ok {
				// A call already keyed: an id-bearing continuation, or an
				// opener whose id arrived before its index. Neither re-aliases
				// the wire index; only an opener may, or a continuation of
				// the first call would steal the alias from the call opened
				// after it.
				idx = known
			} else {
				if owner, taken := t.idByIndex[wire]; taken && owner != tc.ID {
					idx = -1 - len(t.idxByCallID)
				}
				t.idxByCallID[tc.ID] = idx
				t.idByIndex[wire] = tc.ID
				t.aliasOf[wire] = idx
			}
		} else if alias, ok := t.aliasOf[wire]; ok {
			idx = alias
		} else if _, open := t.toolBlockByOAIndex[wire]; !open && t.lastToolOAIndex < 0 {
			idx = t.lastToolOAIndex
		}
		t.lastToolOAIndex = idx
	case tc.ID == "":
		// Neither index nor id: a continuation of the call being streamed.
	default:
		idx, ok := t.idxByCallID[tc.ID]
		if !ok {
			idx = -1 - len(t.idxByCallID)
			t.idxByCallID[tc.ID] = idx
		}
		t.lastToolOAIndex = idx
	}
	return t.lastToolOAIndex
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
// the first time any content is processed. input_tokens is whatever the stream
// has revealed by then, usually 0: OpenAI streaming normally withholds the
// prompt count until the terminal usage chunk, which is why message_delta
// carries the authoritative figure.
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
			Usage:        t.startUsage(),
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
// fragment OpenAI streams for that index, and so does the thought signature
// when the call carries one: the block's id is the only field that can hold
// it, and it is fixed at content_block_start, so a signature arriving on a
// later fragment would have nowhere to go.
func (t *StreamTranslator) openToolBlock(buf *bytes.Buffer, oaIndex int, id, name, signature string) error {
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
		id = syntheticToolUseID(t.messageID, t.curIndex)
	}
	return writeEvent(buf, "content_block_start", contentBlockStartEvent{
		Type:  "content_block_start",
		Index: t.curIndex,
		ContentBlock: contentBlock{
			Type:  "tool_use",
			ID:    signedToolUseID(id, signature),
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

	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			t.promptTokens = chunk.Usage.PromptTokens
			t.cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			t.completionTokens = chunk.Usage.CompletionTokens
		}
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

	// Tool-call deltas. This assumes OpenAI streams each tool call's fragments
	// contiguously, as the spec does: one call runs to completion before the
	// next, keyed by Index. Truly interleaved fragments across two open tool
	// blocks would emit input_json_delta against a closed Anthropic block.
	for _, tc := range choice.Delta.ToolCalls {
		if err := t.ensureStarted(&buf); err != nil {
			return nil, err
		}
		sig := egress.ThoughtSignatureIn(tc.ExtraContent)
		oaIndex := t.oaIndexFor(tc)
		blockIdx, open := t.toolBlockByOAIndex[oaIndex]
		if !open {
			// First fragment for this tool call: open the block (carries id/name).
			if err := t.openToolBlock(&buf, oaIndex, tc.ID, tc.Function.Name, sig); err != nil {
				return nil, err
			}
			blockIdx = t.curIndex
		} else if sig != "" {
			t.lateSignatures++
		}
		// Argument fragments stream as input_json_delta partial JSON.
		if tc.Function.Arguments != "" {
			if err := writeEvent(&buf, "content_block_delta", contentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: blockIdx,
				Delta: contentDelta{Type: "input_json_delta", PartialJSON: string(tc.Function.Arguments)},
			}); err != nil {
				return nil, err
			}
		}
	}

	return buf.Bytes(), nil
}

// Finish emits the terminal events: it closes any open content block, then
// message_delta (stop_reason + the prompt/completion token counts) and
// message_stop. It
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
	if len(t.toolBlockByOAIndex) > 0 && (t.finishReason == "" || t.finishReason == "stop") {
		// A turn that produced tool calls stops for tool_use when the
		// finish_reason claims an ordinary end: some OpenAI-compatible servers
		// report "stop" beside tool_calls, and an agent loop keyed on
		// stop_reason would end the turn without running them. "length" and
		// "content_filter" stand: a call cut short is not one to run.
		stop = "tool_use"
	}
	if err := writeEvent(&buf, "message_delta", messageDeltaEvent{
		Type:  "message_delta",
		Delta: messageDeltaBody{StopReason: &stop, StopSequence: nil},
		Usage: t.deltaUsage(),
	}); err != nil {
		return nil, err
	}
	if err := writeEvent(&buf, "message_stop", messageStopEvent{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// startUsage is the message_start usage: the prompt split the Anthropic way
// (see splitPrompt) and no output yet.
func (t *StreamTranslator) startUsage() usage {
	input, cacheRead := splitPrompt(t.promptTokens, t.cachedTokens)
	return usage{InputTokens: input, CacheReadInputTokens: cacheRead}
}

// deltaUsage is the cumulative message_delta usage, split like startUsage.
func (t *StreamTranslator) deltaUsage() messageDeltaUsage {
	input, cacheRead := splitPrompt(t.promptTokens, t.cachedTokens)
	return messageDeltaUsage{InputTokens: input, CacheReadInputTokens: cacheRead, OutputTokens: t.completionTokens}
}
