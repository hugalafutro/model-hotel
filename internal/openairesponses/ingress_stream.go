package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// IngressStreamTranslator converts the chat.completion.chunk SSE stream the
// pipeline produces into the Responses API event stream a /v1/responses client
// reads. It is single-goroutine, driven by the ingress writer one chunk payload
// at a time.
//
// The item model: at most one reasoning or message item is open at a time,
// and a delta of the other kind closes it with its done events before the
// next opens. Function calls stay open until the turn finishes, whatever
// arrives between their fragments, since chat providers may interleave the
// argument fragments of parallel calls and may put text between them: each
// call therefore closes exactly once, with its FULL arguments on
// output_item.done, which is where Codex reads tool calls from (it ignores
// the argument deltas). The terminal event is emitted on Finish, after the
// usage chunk that follows the finish_reason chunk has arrived.
type IngressStreamTranslator struct {
	responseID string
	model      string
	createdAt  int64
	seq        int

	started  bool // response.created emitted
	finished bool // terminal event emitted

	items         []openItem     // every item in output order, closed or open
	open          int            // index into items of the open reasoning/message item, or -1
	toolItemByIdx map[int]int    // chat tool_calls index -> items index
	idxByCallID   map[string]int // chat tool_call id -> chat index, for fragments sent without one
	finishReason  string
	usage         *Usage
	facts         *RequestFacts
}

// openItem is one output item under construction or already closed.
type openItem struct {
	kind   string // reasoning | message | function_call
	id     string
	text   string // summary text, message text, or arguments
	callID string
	name   string
	closed bool
	// A message carries its content parts in the order they opened:
	// "text" and/or "refusal". refusal is the refusal part's text.
	parts   []string
	refusal string
}

// partIndex is the content_index of a message part, or -1 when the message
// has no such part yet.
func (it *openItem) partIndex(kind string) int {
	for i, p := range it.parts {
		if p == kind {
			return i
		}
	}
	return -1
}

const openIndexNone = -1

// NewIngressStreamTranslator builds a translator for one response. responseID
// and model are what the client sees on every snapshot; facts echoes the
// request's members and reverses the flat chat names of namespaced tools.
func NewIngressStreamTranslator(responseID, model string, facts *RequestFacts) *IngressStreamTranslator {
	return &IngressStreamTranslator{
		responseID:    responseID,
		model:         model,
		createdAt:     time.Now().Unix(),
		open:          openIndexNone,
		toolItemByIdx: map[int]int{},
		idxByCallID:   map[string]int{},
		facts:         facts,
	}
}

// chatInChunk is the chat chunk the translator reads. Usage is decoded on its
// own so a count spelled differently costs the usage and never the delta.
type chatInChunk struct {
	Choices []struct {
		Delta struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			Reasoning        string         `json:"reasoning"`
			Refusal          string         `json:"refusal"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
}

// Translate processes one chat chunk payload (the JSON after "data:") and
// returns the Responses SSE bytes to forward. A payload this translator cannot
// decode is returned as an error for the writer to report; the translator's
// state is untouched by it, so the next frame translates as if the bad one
// had never arrived. The [DONE] sentinel is the writer's to handle, via Finish.
func (t *IngressStreamTranslator) Translate(payload []byte) ([]byte, error) {
	if t.finished {
		return nil, nil
	}
	var chunk chatInChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid chat chunk: %s", jsonfault.Describe(err, len(payload)))
	}
	var buf bytes.Buffer
	t.ensureStarted(&buf)
	if u := translateChatUsage(chunk.Usage); u != nil {
		t.usage = u
	}
	for _, choice := range chunk.Choices {
		// Content after the finish chunk is a provider quirk; the response is
		// already closed for output and only the usage is still read.
		if t.finishReason != "" {
			break
		}
		d := choice.Delta
		if r := d.ReasoningContent; r != "" || d.Reasoning != "" {
			if r == "" {
				r = d.Reasoning
			}
			t.reasoningDelta(&buf, r)
		}
		if d.Content != "" {
			t.textDelta(&buf, d.Content)
		}
		if d.Refusal != "" {
			t.refusalDelta(&buf, d.Refusal)
		}
		for _, tc := range d.ToolCalls {
			t.toolCallDelta(&buf, tc)
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			t.finishReason = *choice.FinishReason
			t.closeOpen(&buf)
		}
	}
	return buf.Bytes(), nil
}

// Finish closes any open item and emits the terminal event exactly once:
// response.completed, or response.incomplete when the provider stopped on
// length. It returns nothing on a stream that already ended in failure.
func (t *IngressStreamTranslator) Finish() ([]byte, error) {
	if t.finished {
		return nil, nil
	}
	var buf bytes.Buffer
	t.ensureStarted(&buf)
	t.closeOpen(&buf)
	t.finished = true
	resp := t.snapshot("completed")
	resp.Status, resp.IncompleteDetails = statusForFinish(t.finishReason)
	resp.Usage = t.usage
	eventType := "response.completed"
	if resp.Status == "incomplete" {
		eventType = "response.incomplete"
	}
	t.emit(&buf, eventType, map[string]any{"response": resp})
	return buf.Bytes(), nil
}

// Fail ends the stream with response.failed carrying the error, the event a
// Responses client (and Codex) reads a failed generation from. code is the
// gateway's error kind when it has one. Emits nothing after the stream ended.
func (t *IngressStreamTranslator) Fail(message, code string) []byte {
	if t.finished {
		return nil
	}
	var buf bytes.Buffer
	t.ensureStarted(&buf)
	t.closeOpen(&buf)
	t.finished = true
	resp := t.snapshot("failed")
	resp.Usage = t.usage
	if code == "" {
		code = "server_error"
	}
	resp.Error = &ResponseError{Code: code, Message: message}
	t.emit(&buf, "response.failed", map[string]any{"response": resp})
	return buf.Bytes()
}

// BuildStreamFailure is the response.failed frame for a stream this gateway
// never translated (the native passthrough), where no translator holds the
// sequence. responseID and sequence are what the forwarded upstream events
// carried (InspectStreamEvent reads them), so the frame continues the stream
// the client was reading rather than starting a second one.
func BuildStreamFailure(message, code, responseID string, sequence int) []byte {
	if code == "" {
		code = "server_error"
	}
	resp := newResponseObject(responseID, "", "failed", time.Now().Unix(), nil)
	resp.Error = &ResponseError{Code: code, Message: message}
	var buf bytes.Buffer
	writeEvent(&buf, "response.failed", map[string]any{"type": "response.failed", "sequence_number": sequence, "response": resp})
	return buf.Bytes()
}

func (t *IngressStreamTranslator) ensureStarted(buf *bytes.Buffer) {
	if t.started {
		return
	}
	t.started = true
	t.emit(buf, "response.created", map[string]any{"response": t.snapshot("in_progress")})
	t.emit(buf, "response.in_progress", map[string]any{"response": t.snapshot("in_progress")})
}

// snapshot is the Response object as of now: every item so far, in order,
// closed ones completed and the open one in progress.
func (t *IngressStreamTranslator) snapshot(status string) responseObject {
	resp := newResponseObject(t.responseID, t.model, status, t.createdAt, t.facts)
	for _, it := range t.items {
		resp.Output = append(resp.Output, t.itemObject(it))
	}
	return resp
}

func (t *IngressStreamTranslator) itemObject(it openItem) any {
	status := "in_progress"
	if it.closed {
		status = "completed"
	}
	switch it.kind {
	case "reasoning":
		return newReasoningItem(it.id, status, it.text)
	case "message":
		var content []any
		for _, p := range it.parts {
			if p == "text" {
				content = append(content, newOutputText(it.text))
			} else {
				content = append(content, refusalPart{Type: "refusal", Refusal: it.refusal})
			}
		}
		return newMessageItem(it.id, status, content)
	default:
		args := it.text
		if it.closed {
			args = normaliseArguments(util.ToolArguments(args))
		}
		return newFunctionCallItem(it.id, status, it.callID, it.name, args, t.facts)
	}
}

func (t *IngressStreamTranslator) reasoningDelta(buf *bytes.Buffer, delta string) {
	if t.open == openIndexNone || t.items[t.open].kind != "reasoning" {
		t.closeNonTool(buf)
		idx := t.openItem(buf, openItem{kind: "reasoning", id: "rs_" + uuidHex()})
		t.emit(buf, "response.reasoning_summary_part.added", map[string]any{
			"item_id": t.items[idx].id, "output_index": idx, "summary_index": 0,
			"part": SummaryPart{Type: "summary_text", Text: ""},
		})
	}
	it := &t.items[t.open]
	it.text += delta
	t.emit(buf, "response.reasoning_summary_text.delta", map[string]any{
		"item_id": it.id, "output_index": t.open, "summary_index": 0, "delta": delta,
	})
}

// openMessage makes a message item the open item, closing an open reasoning
// item first, and returns its index.
func (t *IngressStreamTranslator) openMessage(buf *bytes.Buffer) int {
	if t.open == openIndexNone || t.items[t.open].kind != "message" {
		t.closeNonTool(buf)
		t.openItem(buf, openItem{kind: "message", id: "msg_" + uuidHex()})
	}
	return t.open
}

// openPart adds a content part to the open message the first time text of
// its kind arrives, and returns the part's content_index.
func (t *IngressStreamTranslator) openPart(buf *bytes.Buffer, idx int, kind string, part any) int {
	it := &t.items[idx]
	if ci := it.partIndex(kind); ci >= 0 {
		return ci
	}
	it.parts = append(it.parts, kind)
	ci := len(it.parts) - 1
	t.emit(buf, "response.content_part.added", map[string]any{
		"item_id": it.id, "output_index": idx, "content_index": ci, "part": part,
	})
	return ci
}

func (t *IngressStreamTranslator) textDelta(buf *bytes.Buffer, delta string) {
	idx := t.openMessage(buf)
	ci := t.openPart(buf, idx, "text", newOutputText(""))
	it := &t.items[idx]
	it.text += delta
	t.emit(buf, "response.output_text.delta", map[string]any{
		"item_id": it.id, "output_index": idx, "content_index": ci, "delta": delta, "logprobs": []any{},
	})
}

// refusalDelta streams a refusal as the message's refusal part, the shape the
// non-streaming builder gives it, so both forms of the answer agree.
func (t *IngressStreamTranslator) refusalDelta(buf *bytes.Buffer, delta string) {
	idx := t.openMessage(buf)
	ci := t.openPart(buf, idx, "refusal", refusalPart{Type: "refusal", Refusal: ""})
	it := &t.items[idx]
	it.refusal += delta
	t.emit(buf, "response.refusal.delta", map[string]any{
		"item_id": it.id, "output_index": idx, "content_index": ci, "delta": delta,
	})
}

// toolCallDelta routes one chat tool_calls fragment. The first fragment for
// an index carries the id and name and opens the item; later ones carry
// argument text. Function calls close only when the turn finishes, so every
// fragment lands on the one item its index (or call id) names.
func (t *IngressStreamTranslator) toolCallDelta(buf *bytes.Buffer, tc chatToolCall) {
	chatIdx := t.chatIndexFor(tc)
	idx, known := t.toolItemByIdx[chatIdx]
	if !known {
		t.closeNonTool(buf)
		idx = t.openItem(buf, openItem{kind: "function_call", id: "fc_" + uuidHex(), callID: tc.ID, name: tc.Function.Name})
		t.toolItemByIdx[chatIdx] = idx
	}
	it := &t.items[idx]
	if it.callID == "" {
		it.callID = tc.ID
	}
	if it.name == "" {
		it.name = tc.Function.Name
	}
	if args := string(tc.Function.Arguments); args != "" {
		it.text += args
		t.emit(buf, "response.function_call_arguments.delta", map[string]any{
			"item_id": it.id, "output_index": idx, "delta": args,
		})
	}
}

// chatIndexFor is the chat tool_calls index a fragment belongs to. A provider
// that omits the index is read by call id instead, each id getting its own
// synthetic index, so two parallel calls sent without indexes do not merge
// into one item; a fragment with neither is the first call.
func (t *IngressStreamTranslator) chatIndexFor(tc chatToolCall) int {
	if tc.Index != nil {
		if tc.ID != "" {
			t.idxByCallID[tc.ID] = *tc.Index
		}
		return *tc.Index
	}
	if tc.ID == "" {
		return 0
	}
	if idx, ok := t.idxByCallID[tc.ID]; ok {
		return idx
	}
	idx := -1 - len(t.idxByCallID)
	t.idxByCallID[tc.ID] = idx
	return idx
}

// openItem appends an item and emits its output_item.added. A reasoning or
// message item becomes the open one; a function call is tracked by its chat
// index instead.
func (t *IngressStreamTranslator) openItem(buf *bytes.Buffer, it openItem) int {
	t.items = append(t.items, it)
	idx := len(t.items) - 1
	if it.kind != "function_call" {
		t.open = idx
	}
	t.emit(buf, "response.output_item.added", map[string]any{
		"output_index": idx, "item": t.itemObject(it),
	})
	return idx
}

// closeOpen closes everything still open, in output order: the reasoning or
// message item and every open function call.
func (t *IngressStreamTranslator) closeOpen(buf *bytes.Buffer) {
	t.closeNonTool(buf)
	for idx := range t.items {
		if !t.items[idx].closed {
			t.closeItem(buf, idx)
		}
	}
}

// closeNonTool closes the open reasoning or message item, if any, leaving
// open function calls to accumulate.
func (t *IngressStreamTranslator) closeNonTool(buf *bytes.Buffer) {
	if t.open == openIndexNone {
		return
	}
	idx := t.open
	t.open = openIndexNone
	t.closeItem(buf, idx)
}

// closeItem emits the done events of one item.
func (t *IngressStreamTranslator) closeItem(buf *bytes.Buffer, idx int) {
	it := &t.items[idx]
	it.closed = true
	switch it.kind {
	case "reasoning":
		t.emit(buf, "response.reasoning_summary_text.done", map[string]any{
			"item_id": it.id, "output_index": idx, "summary_index": 0, "text": it.text,
		})
		t.emit(buf, "response.reasoning_summary_part.done", map[string]any{
			"item_id": it.id, "output_index": idx, "summary_index": 0,
			"part": SummaryPart{Type: "summary_text", Text: it.text},
		})
	case "message":
		for ci, p := range it.parts {
			var part any
			if p == "text" {
				part = newOutputText(it.text)
				t.emit(buf, "response.output_text.done", map[string]any{
					"item_id": it.id, "output_index": idx, "content_index": ci, "text": it.text, "logprobs": []any{},
				})
			} else {
				part = refusalPart{Type: "refusal", Refusal: it.refusal}
				t.emit(buf, "response.refusal.done", map[string]any{
					"item_id": it.id, "output_index": idx, "content_index": ci, "refusal": it.refusal,
				})
			}
			t.emit(buf, "response.content_part.done", map[string]any{
				"item_id": it.id, "output_index": idx, "content_index": ci, "part": part,
			})
		}
	default:
		done := newFunctionCallItem(it.id, "completed", it.callID, it.name, normaliseArguments(util.ToolArguments(it.text)), t.facts)
		t.emit(buf, "response.function_call_arguments.done", map[string]any{
			"item_id": it.id, "output_index": idx, "name": done.Name, "arguments": done.Arguments,
		})
	}
	t.emit(buf, "response.output_item.done", map[string]any{
		"output_index": idx, "item": t.itemObject(*it),
	})
}

// emit frames one event: the event line, then the payload stamped with its
// type and the next sequence number.
func (t *IngressStreamTranslator) emit(buf *bytes.Buffer, eventType string, payload map[string]any) {
	payload["type"] = eventType
	payload["sequence_number"] = t.seq
	t.seq++
	writeEvent(buf, eventType, payload)
}

func writeEvent(buf *bytes.Buffer, eventType string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		// Every payload here is built from fixed, marshalable shapes; a
		// failure degrades to a skipped frame rather than a corrupt stream.
		debuglog.Warn("openairesponses: marshal ingress event failed", "event", eventType, "error", err)
		return
	}
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
}
