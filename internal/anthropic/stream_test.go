package anthropic

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// decodeWithSDK feeds raw Anthropic SSE bytes through the real anthropic-sdk-go
// stream decoder, proving a genuine Anthropic SDK client accepts our output. It
// returns the ordered event types plus the reconstructed assistant turn.
type decoded struct {
	eventTypes   []string
	text         string
	toolJSONByIx map[int]string
	toolNameByIx map[int]string
	stopReason   string
	outputTokens int64
	model        string
	msgID        string
}

func decodeWithSDK(t *testing.T, sse []byte) decoded {
	t.Helper()
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(bytes.NewReader(sse)),
	}
	stream := ssestream.NewStream[sdk.MessageStreamEventUnion](ssestream.NewDecoder(resp), nil)
	out := decoded{toolJSONByIx: map[int]string{}, toolNameByIx: map[int]string{}}
	for stream.Next() {
		ev := stream.Current()
		out.eventTypes = append(out.eventTypes, ev.Type)
		switch ev.Type {
		case "message_start":
			ms := ev.AsMessageStart()
			out.model = ms.Message.Model
			out.msgID = ms.Message.ID
		case "content_block_start":
			cbs := ev.AsContentBlockStart()
			if cbs.ContentBlock.Type == "tool_use" {
				tu := cbs.ContentBlock.AsToolUse()
				out.toolNameByIx[int(cbs.Index)] = tu.Name
			}
		case "content_block_delta":
			cbd := ev.AsContentBlockDelta()
			switch cbd.Delta.Type {
			case "text_delta":
				out.text += cbd.Delta.Text
			case "input_json_delta":
				out.toolJSONByIx[int(cbd.Index)] += cbd.Delta.PartialJSON
			}
		case "message_delta":
			md := ev.AsMessageDelta()
			out.stopReason = string(md.Delta.StopReason)
			out.outputTokens = md.Usage.OutputTokens
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("SDK stream decode error: %v\n---SSE---\n%s", err, sse)
	}
	return out
}

// runTranslator drives a translator over the given chunks and concatenates all
// emitted SSE (Translate outputs followed by Finish).
func runTranslator(t *testing.T, tr *StreamTranslator, chunks []OAStreamChunk) []byte {
	t.Helper()
	var buf bytes.Buffer
	for i, c := range chunks {
		b, err := tr.Translate(c)
		if err != nil {
			t.Fatalf("Translate chunk %d: %v", i, err)
		}
		buf.Write(b)
	}
	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	buf.Write(fin)
	return buf.Bytes()
}

func TestStreamTranslator_TextOnly_AcceptedBySDK(t *testing.T) {
	tr := NewStreamTranslator("msg_test123", "claude-sonnet-4-6")
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{Content: "Hello"}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{Content: ", world"}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{Content: "!"}, FinishReason: new("stop")}}},
		{Usage: &OAUsage{PromptTokens: 9, CompletionTokens: 3}},
	}
	sse := runTranslator(t, tr, chunks)

	got := decodeWithSDK(t, sse)
	if got.text != "Hello, world!" {
		t.Errorf("text = %q, want %q", got.text, "Hello, world!")
	}
	if got.stopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", got.stopReason)
	}
	if got.outputTokens != 3 {
		t.Errorf("output_tokens = %d, want 3", got.outputTokens)
	}
	if got.model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", got.model)
	}
	if got.msgID != "msg_test123" {
		t.Errorf("msg id = %q, want msg_test123", got.msgID)
	}

	// We emit a ping after message_start for fidelity with the real API; the
	// SDK decoder treats ping as a keepalive and does not surface it as a typed
	// event, so assert it on the raw wire instead.
	if !bytes.Contains(sse, []byte("event: ping")) {
		t.Errorf("raw SSE missing ping keepalive")
	}

	// Typed-event ordering contract (ping excluded, per above).
	want := []string{
		"message_start",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_delta",
		"content_block_stop", "message_delta", "message_stop",
	}
	if strings.Join(got.eventTypes, ",") != strings.Join(want, ",") {
		t.Errorf("event order:\n got %v\nwant %v", got.eventTypes, want)
	}
}

func TestStreamTranslator_ToolUse_AcceptedBySDK(t *testing.T) {
	tr := NewStreamTranslator("msg_tool", "claude-sonnet-4-6")
	chunks := []OAStreamChunk{
		// some preamble text
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{Content: "Let me check."}}}},
		// tool call: name+id on first fragment, args streamed in pieces
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: 0, ID: "call_abc", Type: "function", Function: OAFunctionDelta{Name: "get_weather"}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: 0, Function: OAFunctionDelta{Arguments: `{"city":`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: 0, Function: OAFunctionDelta{Arguments: `"Paris"}`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("tool_calls")}}},
		{Usage: &OAUsage{PromptTokens: 20, CompletionTokens: 12}},
	}
	sse := runTranslator(t, tr, chunks)

	got := decodeWithSDK(t, sse)
	if got.text != "Let me check." {
		t.Errorf("text = %q, want %q", got.text, "Let me check.")
	}
	if got.stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", got.stopReason)
	}
	// Tool block is index 1 (text was index 0).
	if name := got.toolNameByIx[1]; name != "get_weather" {
		t.Errorf("tool name @1 = %q, want get_weather", name)
	}
	if js := got.toolJSONByIx[1]; js != `{"city":"Paris"}` {
		t.Errorf("tool input json @1 = %q, want %q", js, `{"city":"Paris"}`)
	}
}

func TestStreamTranslator_EmptyCompletion_WellFormed(t *testing.T) {
	tr := NewStreamTranslator("msg_empty", "claude-haiku-4-5")
	// No content at all, just a terminal finish.
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("stop")}}},
	}
	sse := runTranslator(t, tr, chunks)
	got := decodeWithSDK(t, sse)
	if got.text != "" {
		t.Errorf("text = %q, want empty", got.text)
	}
	if len(got.eventTypes) == 0 || got.eventTypes[0] != "message_start" {
		t.Errorf("first event = %v, want message_start first", got.eventTypes)
	}
	last := got.eventTypes[len(got.eventTypes)-1]
	if last != "message_stop" {
		t.Errorf("last event = %q, want message_stop", last)
	}
}
