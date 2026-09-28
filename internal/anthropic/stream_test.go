package anthropic

import (
	"bytes"
	"encoding/json"
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
	inputTokens  int64
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
			out.inputTokens = md.Usage.InputTokens
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
	// The prompt count only shows up in the terminal OpenAI usage chunk, so
	// message_delta is where it has to surface; reporting 0 here made every
	// streamed Messages response look like it had no prompt at all.
	if got.inputTokens != 9 {
		t.Errorf("input_tokens = %d, want 9", got.inputTokens)
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
			{Index: new(0), ID: "call_abc", Type: "function", Function: OAFunctionDelta{Name: "get_weather"}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: new(0), Function: OAFunctionDelta{Arguments: `{"city":`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: new(0), Function: OAFunctionDelta{Arguments: `"Paris"}`}},
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

// Some OpenAI-compatible servers stream tool calls without an index. Two such
// calls must open two tool_use blocks, keyed by id, and a fragment carrying
// neither id nor index continues the call last opened rather than index 0.
func TestStreamTranslator_IndexlessParallelToolCalls(t *testing.T) {
	tr := NewStreamTranslator("msg_noidx", "m")
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{"a":`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Function: OAFunctionDelta{Arguments: `1}`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{ID: "call_b", Type: "function", Function: OAFunctionDelta{Name: "cat", Arguments: `{"b":2}`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("tool_calls")}}},
	}
	got := decodeWithSDK(t, runTranslator(t, tr, chunks))
	if got.toolNameByIx[0] != "ls" || got.toolNameByIx[1] != "cat" {
		t.Fatalf("tool blocks = %v, want ls at 0 and cat at 1 (index-less calls merged)", got.toolNameByIx)
	}
	if got.toolJSONByIx[0] != `{"a":1}` || got.toolJSONByIx[1] != `{"b":2}` {
		t.Errorf("tool inputs = %v, want {\"a\":1} and {\"b\":2}", got.toolJSONByIx)
	}
}

// finish_reason "stop" beside tool calls (some OpenAI-compatible servers report
// it so) still stops the turn for tool_use, or an agent loop keyed on
// stop_reason would end the turn without running the tools.
func TestStreamTranslator_ToolCallsWinOverStopFinish(t *testing.T) {
	tr := NewStreamTranslator("msg_stop", "m")
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: new(0), ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{}`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("stop")}}},
	}
	got := decodeWithSDK(t, runTranslator(t, tr, chunks))
	if got.stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", got.stopReason)
	}
}

// finish_reason "length" beside tool calls stays max_tokens: a call cut
// mid-arguments is not one to run.
func TestStreamTranslator_LengthBesideToolCallsStaysMaxTokens(t *testing.T) {
	tr := NewStreamTranslator("msg_len", "m")
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: new(0), ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{"pa`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("length")}}},
	}
	got := decodeWithSDK(t, runTranslator(t, tr, chunks))
	if got.stopReason != "max_tokens" {
		t.Errorf("stop_reason = %q, want max_tokens", got.stopReason)
	}
}

// Mixed shapes: an id-keyed opener followed by a continuation carrying only
// "index":0 is one call, and two openers both stamped "index":0 with distinct
// ids are two calls.
func TestStreamTranslator_MixedIndexShapes(t *testing.T) {
	t.Run("index appears on the continuation", func(t *testing.T) {
		tr := NewStreamTranslator("msg_mix1", "m")
		chunks := []OAStreamChunk{
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{"a":`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), Function: OAFunctionDelta{Arguments: `1}`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("tool_calls")}}},
		}
		got := decodeWithSDK(t, runTranslator(t, tr, chunks))
		if len(got.toolNameByIx) != 1 || got.toolJSONByIx[0] != `{"a":1}` {
			t.Errorf("blocks = %v inputs = %v, want one call ls with {\"a\":1}", got.toolNameByIx, got.toolJSONByIx)
		}
	})
	t.Run("id keyed first, index and id together later", func(t *testing.T) {
		tr := NewStreamTranslator("msg_mix3", "m")
		chunks := []OAStreamChunk{
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{"a":`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(1), ID: "call_a", Function: OAFunctionDelta{Arguments: `1}`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("tool_calls")}}},
		}
		got := decodeWithSDK(t, runTranslator(t, tr, chunks))
		if len(got.toolNameByIx) != 1 || got.toolJSONByIx[0] != `{"a":1}` {
			t.Errorf("blocks = %v inputs = %v, want one call ls with {\"a\":1}", got.toolNameByIx, got.toolJSONByIx)
		}
	})
	t.Run("index 0 stamped on every call and its continuations", func(t *testing.T) {
		tr := NewStreamTranslator("msg_mix2", "m")
		chunks := []OAStreamChunk{
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{"a":`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), Function: OAFunctionDelta{Arguments: `1}`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), ID: "call_b", Type: "function", Function: OAFunctionDelta{Name: "cat", Arguments: `{"b":`}},
			}}}}},
			// An id-bearing continuation of the first call in between must not
			// take the wire index back from the second.
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), ID: "call_a", Function: OAFunctionDelta{Arguments: ``}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
				{Index: new(0), Function: OAFunctionDelta{Arguments: `2}`}},
			}}}}},
			{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("tool_calls")}}},
		}
		got := decodeWithSDK(t, runTranslator(t, tr, chunks))
		if got.toolNameByIx[0] != "ls" || got.toolNameByIx[1] != "cat" {
			t.Fatalf("blocks = %v, want ls and cat", got.toolNameByIx)
		}
		if got.toolJSONByIx[0] != `{"a":1}` || got.toolJSONByIx[1] != `{"b":2}` {
			t.Errorf("inputs = %v, want the second call's continuation on the second block", got.toolJSONByIx)
		}
	})
}

// finish_reason "content_filter" beside tool calls is not an ordinary end
// either; it stays end_turn rather than telling the client to run the call.
func TestStreamTranslator_ContentFilterBesideToolCallsStaysEndTurn(t *testing.T) {
	tr := NewStreamTranslator("msg_cf", "m")
	chunks := []OAStreamChunk{
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{
			{Index: new(0), ID: "call_a", Type: "function", Function: OAFunctionDelta{Name: "ls", Arguments: `{}`}},
		}}}}},
		{Choices: []OAStreamChoice{{Delta: OAStreamDelta{}, FinishReason: new("content_filter")}}},
	}
	if got := decodeWithSDK(t, runTranslator(t, tr, chunks)); got.stopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", got.stopReason)
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

// An object-form tool call must survive the translation to the Anthropic wire
// format. The OA types are decoded from the upstream JSON, and a plain-string
// Arguments field failed that decode — so the chunk was skipped and the client
// received a message_delta with stop_reason "tool_use" and no tool_use content
// block at all, which the Anthropic SDKs reject.
//
// Decoded from JSON rather than built as Go values on purpose: the bug lived in
// the unmarshal, so constructing OAStreamChunk directly would test nothing.
func TestStreamTranslator_ObjectFormToolArgumentsSurvive(t *testing.T) {
	raw := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	chunks := make([]OAStreamChunk, 0, len(raw))
	for _, r := range raw {
		var c OAStreamChunk
		if err := json.Unmarshal([]byte(r), &c); err != nil {
			t.Fatalf("an object-form tool call must decode: %v", err)
		}
		chunks = append(chunks, c)
	}

	got := decodeWithSDK(t, runTranslator(t, tr(t), chunks))

	if got.stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", got.stopReason)
	}
	if name := got.toolNameByIx[0]; name != "get_weather" {
		t.Errorf("tool name = %q, want get_weather: stop_reason tool_use with no tool_use block is a protocol violation", name)
	}
	if args := got.toolJSONByIx[0]; args != `{"city":"Prague"}` {
		t.Errorf("tool input = %q, want the arguments object", args)
	}
}

func tr(t *testing.T) *StreamTranslator {
	t.Helper()
	return NewStreamTranslator("msg_obj_args", "claude-sonnet-4-6")
}

// A streamed tool call whose opening fragment carries the Gemini 3 thought
// signature opens a tool_use block whose id carries it (the id is fixed at
// content_block_start, so that fragment is the only place it can be read),
// and a call without one keeps the id it came with. Decoded from JSON since
// the carrier is read off the wire.
func TestStreamTranslator_ToolUseIDCarriesThoughtSignature(t *testing.T) {
	tr := NewStreamTranslator("msg_sig", "m")
	var chunks []OAStreamChunk
	for _, raw := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_s","type":"function","function":{"name":"get_weather","arguments":""},"extra_content":{"google":{"thought_signature":"sig-s"}}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_p","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		var c OAStreamChunk
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("chunk %s: %v", raw, err)
		}
		chunks = append(chunks, c)
	}
	sse := runTranslator(t, tr, chunks)
	var ids []string
	for _, line := range strings.Split(string(sse), "\n") {
		if !strings.HasPrefix(line, "data: ") || !strings.Contains(line, `"content_block_start"`) || !strings.Contains(line, `"tool_use"`) {
			continue
		}
		var ev struct {
			ContentBlock struct {
				ID string `json:"id"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("event %s: %v", line, err)
		}
		ids = append(ids, ev.ContentBlock.ID)
	}
	if len(ids) != 2 {
		t.Fatalf("tool_use blocks = %v, want two", ids)
	}
	if id, sig := splitToolUseID(ids[0]); id != "call_s" || sig != "sig-s" {
		t.Errorf("signed block id %q splits to (%q, %q), want (call_s, sig-s)", ids[0], id, sig)
	}
	if ids[1] != "call_p" {
		t.Errorf("unsigned block id = %q, want call_p untouched", ids[1])
	}
}

// A signature on a fragment after the one that opened the call has no
// carrier (the id is fixed at content_block_start); it is counted so the
// proxy can log the loss, and the block keeps the id it opened with.
func TestStreamTranslator_LateSignatureCounted(t *testing.T) {
	tr := NewStreamTranslator("msg_late", "m")
	var chunks []OAStreamChunk
	for _, raw := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_l","type":"function","function":{"name":"f","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"},"extra_content":{"google":{"thought_signature":"late"}}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		var c OAStreamChunk
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}
	sse := runTranslator(t, tr, chunks)
	if tr.LateSignatures() != 1 {
		t.Errorf("late signatures = %d, want 1", tr.LateSignatures())
	}
	if !strings.Contains(string(sse), `"id":"call_l"`) || strings.Contains(string(sse), thoughtSigMarker) {
		t.Errorf("block id should be the bare call_l: %s", sse)
	}
}
