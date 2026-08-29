package anthropicegress

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// --- Incoming Anthropic SSE event shapes ---

// antEvent is the union of every Anthropic streaming event this package
// consumes. Each event populates only the members its own type carries, so one
// decode covers the whole sequence:
//
//	message_start        -> Message.Usage (input-side token counts)
//	content_block_start  -> Index + ContentBlock
//	content_block_delta  -> Index + Delta (text/thinking/partial_json)
//	content_block_stop   -> Index
//	message_delta        -> Delta.StopReason + Usage.OutputTokens
//	message_stop, ping   -> nothing beyond Type
//	error                -> Error
type antEvent struct {
	Type         string           `json:"type"`
	Index        int              `json:"index"`
	Message      *antEventMessage `json:"message"`
	ContentBlock *antRespBlock    `json:"content_block"`
	Delta        *antEventDelta   `json:"delta"`
	// Raw, for the reason on antResponse.Usage: a count spelled differently must
	// not fail the event and kill the stream.
	Usage json.RawMessage `json:"usage"`
	Error *antRespError   `json:"error"`
}

// antEventMessage is the partial Message object message_start carries. Only
// usage is read: id and model are the caller's to choose, and the content array
// is always empty at that point.
type antEventMessage struct {
	Usage json.RawMessage `json:"usage"`
}

// antEventDelta is the delta union shared by content_block_delta (text_delta,
// thinking_delta, input_json_delta, and the signature/citation deltas this
// package ignores) and message_delta (stop_reason).
type antEventDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}

// --- Outgoing OpenAI chat.completion.chunk shape ---

type chunk struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []chunkChoice    `json:"choices"`
	Usage   *completionUsage `json:"usage,omitempty"`
}

type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chunkDelta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []chunkToolCall `json:"tool_calls,omitempty"`
}

// chunkToolCall is one streamed tool-call fragment. Index is the OpenAI
// tool-call ordinal (not Anthropic's content-block index); id and type ride on
// the header fragment only, matching how OpenAI streams tool calls.
type chunkToolCall struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function chunkToolFunction `json:"function"`
}

type chunkToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// StreamTranslator converts an Anthropic Messages SSE stream into an OpenAI
// chat.completion.chunk SSE stream ending in "data: [DONE]".
//
// It is the streaming counterpart of BuildChatCompletion and single-goroutine
// by design: the adapter feeds it one upstream data payload at a time and
// forwards whatever bytes come back.
type StreamTranslator struct {
	id      string
	model   string
	created int64

	started  bool // role delta emitted
	finished bool // terminal chunk + [DONE] already emitted
	failed   bool // an error event arrived; no clean finish may follow

	stopReason string // stop_reason from message_delta

	// Anthropic reports input counts on message_start and output tokens on
	// message_delta, so both halves accumulate here and the terminal chunk
	// runs them through translateUsage's arithmetic. A usage object carrying
	// nothing but zeros leaves this zero-valued, and the terminal chunk then
	// omits usage rather than reporting a fabricated 0/0/0.
	usage antRespUsage

	// Anthropic's content-block indices count every block; OpenAI's
	// tool_calls[].index counts only tool calls. This maps one to the other so
	// a response interleaving text and two tool calls emits tool-call indices
	// 0 and 1.
	toolIndexByBlock map[int]int
	toolCalls        int
}

// NewStreamTranslator builds a translator for one response. id, model and
// created are echoed in every chunk envelope (the model string the client
// requested, not the id or model Anthropic reports on message_start).
func NewStreamTranslator(id, model string, created int64) *StreamTranslator {
	return &StreamTranslator{
		id:               id,
		model:            model,
		created:          created,
		toolIndexByBlock: map[int]int{},
	}
}

// writeChunk appends one framed SSE chunk ("data: <json>\n\n").
func (t *StreamTranslator) writeChunk(buf *bytes.Buffer, delta chunkDelta, finishReason *string, usage *completionUsage) error {
	if !t.started {
		delta.Role = "assistant"
		t.started = true
	}
	payload, err := json.Marshal(chunk{
		ID:      t.id,
		Object:  "chat.completion.chunk",
		Created: t.created,
		Model:   t.model,
		Choices: []chunkChoice{{Index: 0, Delta: delta, FinishReason: finishReason}},
		Usage:   usage,
	})
	if err != nil {
		return fmt.Errorf("anthropicegress: marshal stream chunk: %w", err)
	}
	buf.WriteString("data: ")
	buf.Write(payload)
	buf.WriteString("\n\n")
	return nil
}

// Translate processes one Anthropic SSE data payload and returns the chunk
// bytes to forward to the client (often empty: ping, content_block_stop and
// message_delta update state silently). message_stop produces the terminal
// chunk plus "data: [DONE]", after which Finish is a no-op and further payloads
// are ignored: nothing may be emitted after the sentinel, and a truncated line
// trailing a completed stream must not poison it.
func (t *StreamTranslator) Translate(payload []byte) ([]byte, error) {
	if t.finished {
		return nil, nil
	}

	var ev antEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, fmt.Errorf("anthropicegress: invalid stream event: %s", jsonfault.Describe(err, len(payload)))
	}

	var buf bytes.Buffer
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			if u, ok := readEventUsage(ev.Message.Usage); ok {
				t.usage.InputTokens = u.InputTokens
				t.usage.CacheCreationInputTokens = u.CacheCreationInputTokens
				t.usage.CacheReadInputTokens = u.CacheReadInputTokens
			}
		}
		if err := t.writeChunk(&buf, chunkDelta{}, nil, nil); err != nil {
			return nil, err
		}
	case "content_block_start":
		if err := t.startBlock(&buf, ev); err != nil {
			return nil, err
		}
	case "content_block_delta":
		if err := t.blockDelta(&buf, ev); err != nil {
			return nil, err
		}
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != "" {
			t.stopReason = ev.Delta.StopReason
		}
		if u, ok := readEventUsage(ev.Usage); ok {
			t.usage.OutputTokens = u.OutputTokens
		}
	case "message_stop":
		return t.Finish()
	case "error":
		// The stream is dead: the caller must surface a failure, never a
		// terminal chunk that reads as a clean completion. Only the error type
		// is named — error.message can echo request content.
		t.failed = true
		kind := "unknown"
		if ev.Error != nil && ev.Error.Type != "" {
			kind = ev.Error.Type
		}
		return nil, fmt.Errorf("anthropicegress: upstream error: %s", kind)
	case "ping":
		// Anthropic's keepalive across a generation gap, and the gap is longest
		// exactly where this adapter earns its keep: prompt processing after
		// message_start on a large document, and server-side tool pauses. The
		// proxy's stall watchdog is pinged per line of THIS stream, so
		// swallowing the keepalive would let a healthy stream be closed and
		// logged as a provider stall. An SSE comment frame pings it and is
		// ignored by every SSE client, so nothing reaches the caller as a chunk.
		buf.WriteString(": ping\n\n")
	}
	// content_block_stop and any unrecognised event type carry nothing a
	// chat.completion.chunk stream represents.
	return buf.Bytes(), nil
}

// startBlock handles content_block_start. Only tool_use blocks open anything on
// the OpenAI side (the header fragment carrying the tool-call index, id, type
// and name); text and thinking blocks emit content in their deltas alone.
func (t *StreamTranslator) startBlock(buf *bytes.Buffer, ev antEvent) error {
	if ev.ContentBlock == nil || ev.ContentBlock.Type != "tool_use" {
		return nil
	}
	oaIndex := t.toolCalls
	t.toolCalls++
	t.toolIndexByBlock[ev.Index] = oaIndex

	return t.writeChunk(buf, chunkDelta{ToolCalls: []chunkToolCall{{
		Index:    oaIndex,
		ID:       ev.ContentBlock.ID,
		Type:     "function",
		Function: chunkToolFunction{Name: ev.ContentBlock.Name},
	}}}, nil, nil)
}

// blockDelta handles content_block_delta: text and thinking become content and
// reasoning_content deltas, partial JSON becomes a tool-call arguments fragment
// under the tool-call index its content_block_start claimed.
func (t *StreamTranslator) blockDelta(buf *bytes.Buffer, ev antEvent) error {
	if ev.Delta == nil {
		return nil
	}
	var delta chunkDelta
	switch ev.Delta.Type {
	case "text_delta":
		if ev.Delta.Text == "" {
			return nil
		}
		delta.Content = ev.Delta.Text
	case "thinking_delta":
		if ev.Delta.Thinking == "" {
			return nil
		}
		delta.ReasoningContent = ev.Delta.Thinking
	case "input_json_delta":
		if ev.Delta.PartialJSON == "" {
			return nil
		}
		oaIndex, ok := t.toolIndexByBlock[ev.Index]
		if !ok {
			// Arguments for a block no content_block_start opened. Dropping
			// them would hand the client a tool call with silently truncated
			// arguments, so the stream fails instead.
			return errors.New("anthropicegress: tool arguments for an unopened content block")
		}
		delta.ToolCalls = []chunkToolCall{{
			Index:    oaIndex,
			Function: chunkToolFunction{Arguments: ev.Delta.PartialJSON},
		}}
	default:
		// signature_delta, citations_delta and anything newer have no
		// chat-completion equivalent.
		return nil
	}
	return t.writeChunk(buf, delta, nil, nil)
}

// Finish emits the terminal chunk (empty delta, mapped finish_reason, usage
// when the upstream reported any) followed by "data: [DONE]". It is idempotent,
// so a stream that ended on message_stop receives nothing further on EOF, and
// it stays silent after an error event so a failed stream is never closed off
// as a clean one.
func (t *StreamTranslator) Finish() ([]byte, error) {
	if t.finished || t.failed {
		return nil, nil
	}
	t.finished = true

	var buf bytes.Buffer
	reason := mapFinishReason(t.stopReason)
	var usage *completionUsage
	if t.usage != (antRespUsage{}) {
		usage = buildUsage(t.usage)
	}
	if err := t.writeChunk(&buf, chunkDelta{}, &reason, usage); err != nil {
		return nil, err
	}
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes(), nil
}

// readEventUsage decodes a stream event's usage member on its own, reporting
// whether there was one to read.
//
// Separate from the event decode because an error there kills the STREAM: the
// event loses its type along with its counts, so message_stop and the error
// events stop being recognised for that frame. A count the provider spelled
// differently — quoted, or with a fraction on it — is not a reason to do that.
func readEventUsage(raw json.RawMessage) (antRespUsage, bool) {
	if !util.JSONMemberSet(raw) {
		return antRespUsage{}, false
	}
	// A shape error yields NO usage here, unlike proxy.Usage, which keeps
	// whatever decoded. That rule assumes independent members: losing
	// completion_tokens there cannot corrupt prompt_tokens. These figures are
	// DERIVED — summed across members, or falling back to a sum — so a lost
	// addend leaves a number that is wrong AND non-zero, which reads as
	// authoritative and stops estimateMissingUsage ever firing. A cache-read
	// count of 20000 lost that way bills 4.
	//
	// Absent is the honest report, and it is what master did for these bodies
	// too — except master lost the answer with it.
	var u antRespUsage
	if err := util.DecodeCounts(raw, &u); err != nil {
		return antRespUsage{}, false
	}
	return u, true
}
