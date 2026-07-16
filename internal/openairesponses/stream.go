package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// StreamTranslator converts the Responses API typed SSE event stream into the
// chat.completion.chunk SSE stream the rest of the pipeline (and the client)
// speaks. It is single-goroutine: the StreamAdapter drives it from one read
// loop. Events are dispatched on the JSON "type" field, so SSE "event:" lines
// can be ignored; unknown event types are dropped silently (OpenAI adds new
// ones over time, plan §13).
type StreamTranslator struct {
	id      string
	model   string
	created int64

	roleSent bool // first emitted delta carries role:"assistant"
	finished bool // terminal chunks + [DONE] already emitted

	// Tool-call bookkeeping: Responses items stream under their own
	// output_index; map that to the chat tool_calls index we assigned.
	toolIndexByOutput map[int]int
	nextToolIndex     int
	sawToolCall       bool

	// Reasoning summaries stream in parts; separate consecutive parts with a
	// blank line so multi-part summaries stay readable as one field.
	summaryParts int
}

// NewStreamTranslator builds a translator for one response. model is echoed
// in every chunk (the model string the client requested).
func NewStreamTranslator(model string) *StreamTranslator {
	return &StreamTranslator{
		id:                "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		model:             model,
		created:           time.Now().Unix(),
		toolIndexByOutput: map[int]int{},
	}
}

// streamEvent is the union of all Responses SSE payload fields the translator
// reads. Delta is a plain string for both output_text and function-arguments
// deltas.
type streamEvent struct {
	Type        string      `json:"type"`
	Delta       string      `json:"delta"`
	OutputIndex int         `json:"output_index"`
	Item        *OutputItem `json:"item"`
	Response    *Response   `json:"response"`
	// top-level "error" event fields
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TranslateEvent processes one Responses SSE data payload and returns the
// chat-completions SSE bytes to forward (possibly empty). After the terminal
// response.* event it emits the finish chunk, a usage chunk and [DONE]; any
// later events are ignored.
func (t *StreamTranslator) TranslateEvent(data []byte) ([]byte, error) {
	if t.finished {
		return nil, nil
	}
	var ev streamEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid stream event: %w", err)
	}

	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		return t.deltaChunk(chatDelta{Content: ev.Delta}), nil

	case "response.reasoning_summary_text.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		return t.deltaChunk(chatDelta{ReasoningContent: ev.Delta}), nil

	case "response.reasoning_summary_part.added":
		t.summaryParts++
		if t.summaryParts > 1 {
			return t.deltaChunk(chatDelta{ReasoningContent: "\n\n"}), nil
		}
		return nil, nil

	case "response.output_item.added":
		if ev.Item == nil || ev.Item.Type != "function_call" {
			return nil, nil
		}
		idx := t.toolIndex(ev.OutputIndex)
		id := ev.Item.CallID
		if id == "" {
			id = ev.Item.ID
		}
		return t.deltaChunk(chatDelta{ToolCalls: []chatToolCall{{
			Index:    &idx,
			ID:       id,
			Type:     "function",
			Function: chatToolCallFunc{Name: ev.Item.Name},
		}}}), nil

	case "response.function_call_arguments.delta":
		if ev.Delta == "" {
			return nil, nil
		}
		idx := t.toolIndex(ev.OutputIndex)
		return t.deltaChunk(chatDelta{ToolCalls: []chatToolCall{{
			Index:    &idx,
			Function: chatToolCallFunc{Arguments: ev.Delta},
		}}}), nil

	case "response.completed", "response.incomplete":
		return t.finishChunks(ev.Response), nil

	case "response.failed":
		msg := "upstream response failed"
		if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "" {
			msg = ev.Response.Error.Message
		}
		return t.errorChunks(msg), nil

	case "error":
		msg := ev.Message
		if msg == "" {
			msg = "upstream stream error"
		}
		return t.errorChunks(msg), nil
	}

	return nil, nil
}

// toolIndex returns the chat tool_calls index assigned to a Responses
// output_index, allocating the next one on first sight.
func (t *StreamTranslator) toolIndex(outputIndex int) int {
	if idx, ok := t.toolIndexByOutput[outputIndex]; ok {
		return idx
	}
	idx := t.nextToolIndex
	t.nextToolIndex++
	t.toolIndexByOutput[outputIndex] = idx
	t.sawToolCall = true
	return idx
}

// deltaChunk frames one delta as a chat chunk SSE frame, attaching the
// assistant role to the first emitted delta.
func (t *StreamTranslator) deltaChunk(delta chatDelta) []byte {
	if !t.roleSent {
		t.roleSent = true
		delta.Role = "assistant"
	}
	return t.frame(chatChunk{
		ID:      t.id,
		Object:  "chat.completion.chunk",
		Created: t.created,
		Model:   t.model,
		Choices: []chatChunkChoice{{Index: 0, Delta: delta, FinishReason: nil}},
	})
}

// finishChunks emits the terminal sequence: a finish_reason chunk, a
// usage-only chunk (mirroring stream_options.include_usage, which the
// metering pipeline reads), and the [DONE] sentinel.
func (t *StreamTranslator) finishChunks(resp *Response) []byte {
	t.finished = true
	var buf bytes.Buffer

	status := ""
	var details *IncompleteDetails
	var usage *chatUsage
	if resp != nil {
		status = resp.Status
		details = resp.IncompleteDetails
		usage = translateUsage(resp.Usage)
	}
	finish := mapStatusFinishReason(status, details, t.sawToolCall)

	delta := chatDelta{}
	if !t.roleSent {
		// Empty completion: the client still gets a well-formed stream.
		t.roleSent = true
		delta.Role = "assistant"
	}
	buf.Write(t.frame(chatChunk{
		ID:      t.id,
		Object:  "chat.completion.chunk",
		Created: t.created,
		Model:   t.model,
		Choices: []chatChunkChoice{{Index: 0, Delta: delta, FinishReason: &finish}},
	}))
	if usage != nil {
		buf.Write(t.frame(chatChunk{
			ID:      t.id,
			Object:  "chat.completion.chunk",
			Created: t.created,
			Model:   t.model,
			Choices: []chatChunkChoice{},
			Usage:   usage,
		}))
	}
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes()
}

// errorChunks forwards a terminal upstream error as an OpenAI-style error
// frame (which the streaming pipeline records as the failure cause) and ends
// the stream.
func (t *StreamTranslator) errorChunks(message string) []byte {
	t.finished = true
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "type": "server_error"},
	})
	if err != nil {
		payload = []byte(`{"error":{"message":"upstream stream error","type":"server_error"}}`)
	}
	var buf bytes.Buffer
	buf.WriteString("data: ")
	buf.Write(payload)
	buf.WriteString("\n\ndata: [DONE]\n\n")
	return buf.Bytes()
}

// frame marshals one chunk as an SSE data frame.
func (t *StreamTranslator) frame(chunk chatChunk) []byte {
	data, err := json.Marshal(chunk)
	if err != nil {
		// chatChunk is a fixed, marshalable shape; guard anyway so a failure
		// degrades to a skipped frame instead of a corrupted stream.
		debuglog.Warn("openairesponses: marshal chunk failed", "error", err)
		return nil
	}
	var buf bytes.Buffer
	buf.WriteString("data: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}

// Finished reports whether the terminal sequence has been emitted.
func (t *StreamTranslator) Finished() bool { return t.finished }

// StreamAdapter wraps the upstream /v1/responses SSE body as an io.ReadCloser
// that yields chat.completion.chunk SSE bytes. Wrapping the UPSTREAM body (not
// the client writer) lets the whole existing streaming pipeline — TTFT probe,
// stall watchdog, transforms, metering, and any client-side writer such as the
// Anthropic one — run unchanged on what it already understands.
//
// A truncated upstream (EOF before response.completed) surfaces as a stream
// without [DONE], which the pipeline already classifies as a truncation.
type StreamAdapter struct {
	upstream io.ReadCloser
	tr       *StreamTranslator

	lineBuf []byte // partial SSE line carried across reads
	pending []byte // translated bytes not yet handed to the caller
	readBuf []byte
	srcErr  error
}

// NewStreamAdapter builds an adapter for one streaming response. model is
// echoed in every emitted chunk.
func NewStreamAdapter(upstream io.ReadCloser, model string) *StreamAdapter {
	return &StreamAdapter{
		upstream: upstream,
		tr:       NewStreamTranslator(model),
		readBuf:  make([]byte, 32*1024),
	}
}

// Read refills the pending buffer from upstream (translating as it goes) and
// copies out. Upstream errors are surfaced only after all translated bytes
// have been drained.
func (a *StreamAdapter) Read(p []byte) (int, error) {
	for len(a.pending) == 0 {
		if a.srcErr != nil {
			return 0, a.srcErr
		}
		n, err := a.upstream.Read(a.readBuf)
		if n > 0 {
			a.consume(a.readBuf[:n])
		}
		if err != nil {
			a.srcErr = err
		}
	}
	n := copy(p, a.pending)
	a.pending = a.pending[n:]
	return n, nil
}

// consume splits incoming bytes into SSE lines and feeds each data payload to
// the translator. "event:"/comment/blank lines are dropped: the payload's own
// "type" field drives dispatch, and the adapter generates its own framing.
func (a *StreamAdapter) consume(p []byte) {
	a.lineBuf = append(a.lineBuf, p...)
	for {
		idx := bytes.IndexByte(a.lineBuf, '\n')
		if idx < 0 {
			return
		}
		line := bytes.TrimRight(a.lineBuf[:idx], "\r")
		a.lineBuf = a.lineBuf[idx+1:]
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue // Responses streams do not use the [DONE] sentinel; ignore defensively
		}
		out, err := a.tr.TranslateEvent(payload)
		if err != nil {
			debuglog.Warn("openairesponses: stream event translate failed", "error", err)
			continue
		}
		a.pending = append(a.pending, out...)
	}
}

// Close closes the upstream body. The stall watchdog calls this to unblock a
// hung read, so it must propagate to the wrapped connection.
func (a *StreamAdapter) Close() error {
	return a.upstream.Close()
}
