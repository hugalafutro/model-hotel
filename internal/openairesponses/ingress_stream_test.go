package openairesponses

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

// sdkStream is what the real openai-go Responses stream decoder reconstructs
// from the ingress SSE: proof a genuine OpenAI SDK client accepts the output.
type sdkStream struct {
	eventTypes []string
	seqs       []int64
	text       string
	reasoning  string
	// itemsDone is every output_item.done item, in order.
	itemsDone []responses.ResponseOutputItemUnion
	final     *responses.Response
	failed    *responses.Response
}

func decodeWithOpenAISDK(t *testing.T, sse []byte) sdkStream {
	t.Helper()
	// Every event line must name the JSON type: OpenAI sends both, and a
	// client dispatching on either must see the same event.
	for _, block := range bytes.Split(bytes.TrimSpace(sse), []byte("\n\n")) {
		lines := bytes.SplitN(block, []byte("\n"), 2)
		if len(lines) != 2 || !bytes.HasPrefix(lines[0], []byte("event: ")) || !bytes.HasPrefix(lines[1], []byte("data: ")) {
			t.Fatalf("malformed frame:\n%s", block)
		}
		var payload struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(lines[1], []byte("data: ")), &payload); err != nil || payload.Type != string(bytes.TrimPrefix(lines[0], []byte("event: "))) {
			t.Fatalf("event line %q does not match payload type %q", lines[0], payload.Type)
		}
	}
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(sse))}
	stream := ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(resp), nil)
	var out sdkStream
	for stream.Next() {
		ev := stream.Current()
		out.eventTypes = append(out.eventTypes, ev.Type)
		out.seqs = append(out.seqs, ev.SequenceNumber)
		switch ev.Type {
		case "response.output_text.delta":
			out.text += ev.Delta
		case "response.reasoning_summary_text.delta":
			out.reasoning += ev.Delta
		case "response.output_item.done":
			out.itemsDone = append(out.itemsDone, ev.Item)
		case "response.completed", "response.incomplete":
			r := ev.Response
			out.final = &r
		case "response.failed":
			r := ev.Response
			out.failed = &r
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai-go stream decode error: %v\n---SSE---\n%s", err, sse)
	}
	for i := 1; i < len(out.seqs); i++ {
		if out.seqs[i] != out.seqs[i-1]+1 {
			t.Fatalf("sequence_number not contiguous at %d: %v", i, out.seqs)
		}
	}
	if len(out.seqs) > 0 && out.seqs[0] != 0 && !strings.Contains(string(sse), `"status":"failed"`) {
		t.Fatalf("sequence_number must start at 0: %v", out.seqs)
	}
	return out
}

func runIngress(t *testing.T, tr *IngressStreamTranslator, payloads ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	for i, p := range payloads {
		b, err := tr.Translate([]byte(p))
		if err != nil {
			t.Fatalf("Translate %d: %v", i, err)
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

func chunk(delta, finish string) string {
	fr := "null"
	if finish != "" {
		fr = `"` + finish + `"`
	}
	return `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + fr + `}]}`
}

const usageChunk = `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":6},"completion_tokens_details":{"reasoning_tokens":2}}}`

func TestIngressStream_TextOnly_AcceptedBySDK(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "hotel/g", nil)
	sse := runIngress(t, tr,
		chunk(`{"role":"assistant","content":""}`, ""),
		chunk(`{"content":"Hel"}`, ""),
		chunk(`{"content":"lo"}`, ""),
		chunk(`{}`, "stop"),
		usageChunk,
	)
	got := decodeWithOpenAISDK(t, sse)
	want := []string{"response.created", "response.in_progress", "response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.delta", "response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed"}
	if strings.Join(got.eventTypes, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", got.eventTypes)
	}
	if got.text != "Hello" {
		t.Errorf("text = %q", got.text)
	}
	if got.final == nil || got.final.ID != "resp_1" || got.final.Model != "hotel/g" || got.final.Status != "completed" {
		t.Fatalf("final = %+v", got.final)
	}
	if got.final.OutputText() != "Hello" {
		t.Errorf("final output text = %q", got.final.OutputText())
	}
	u := got.final.Usage
	if u.InputTokens != 10 || u.OutputTokens != 4 || u.TotalTokens != 14 || u.InputTokensDetails.CachedTokens != 6 || u.OutputTokensDetails.ReasoningTokens != 2 {
		t.Errorf("usage = %+v", u)
	}
	if len(got.itemsDone) != 1 || got.itemsDone[0].AsMessage().Status != "completed" {
		t.Errorf("items done = %+v", got.itemsDone)
	}
}

func TestIngressStream_ReasoningThenText(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"role":"assistant","reasoning_content":"think "}`, ""),
		chunk(`{"reasoning_content":"hard"}`, ""),
		chunk(`{"content":"answer"}`, "stop"),
	)
	got := decodeWithOpenAISDK(t, sse)
	if got.reasoning != "think hard" || got.text != "answer" {
		t.Fatalf("reasoning=%q text=%q", got.reasoning, got.text)
	}
	if len(got.final.Output) != 2 || got.final.Output[0].Type != "reasoning" || got.final.Output[1].Type != "message" {
		t.Fatalf("output = %+v", got.final.Output)
	}
	rs := got.final.Output[0].AsReasoning()
	if len(rs.Summary) != 1 || rs.Summary[0].Text != "think hard" || !strings.HasPrefix(rs.ID, "rs_") {
		t.Errorf("reasoning item = %+v", rs)
	}
	joined := strings.Join(got.eventTypes, ",")
	for _, ev := range []string{"response.reasoning_summary_part.added", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_summary_part.done"} {
		if !strings.Contains(joined, ev) {
			t.Errorf("missing %s in %v", ev, got.eventTypes)
		}
	}
	// The reasoning item closes before the message opens.
	if strings.Index(joined, "response.reasoning_summary_part.done") > strings.Index(joined, "response.content_part.added") {
		t.Errorf("reasoning must close before the message opens: %v", got.eventTypes)
	}
	if got.final.Usage.TotalTokens != 0 {
		t.Errorf("no usage chunk: usage must be absent, got %+v", got.final.Usage)
	}
}

func TestIngressStream_ParallelToolCalls_FullArgumentsOnDone(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"ls","arguments":""}}]}`, ""),
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\":"}}]}`, ""),
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\".\"}"}}]}`, ""),
		chunk(`{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"cat","arguments":{"f":"x"}}}]}`, ""),
		chunk(`{}`, "tool_calls"),
		usageChunk,
	)
	got := decodeWithOpenAISDK(t, sse)
	if len(got.itemsDone) != 2 {
		t.Fatalf("items done = %d: %v", len(got.itemsDone), got.eventTypes)
	}
	a := got.itemsDone[0].AsFunctionCall()
	b := got.itemsDone[1].AsFunctionCall()
	if a.CallID != "call_a" || a.Name != "ls" || a.Arguments != `{"p":"."}` || a.Status != "completed" || !strings.HasPrefix(a.ID, "fc_") {
		t.Errorf("call a = %+v", a)
	}
	// Object-form arguments become the spec's JSON string.
	if b.CallID != "call_b" || b.Name != "cat" || b.Arguments != `{"f":"x"}` {
		t.Errorf("call b = %+v", b)
	}
	if got.final.Status != "completed" || len(got.final.Output) != 2 || got.final.Output[1].AsFunctionCall().Arguments != `{"f":"x"}` {
		t.Errorf("final = %+v", got.final)
	}
	joined := strings.Join(got.eventTypes, ",")
	if strings.Count(joined, "response.function_call_arguments.delta") != 3 || strings.Count(joined, "response.function_call_arguments.done") != 2 {
		t.Errorf("argument events: %v", got.eventTypes)
	}
	// Parallel calls stay open together and close in output order when the
	// turn finishes: every added precedes every done.
	if strings.LastIndex(joined, "response.output_item.added") > strings.Index(joined, "response.function_call_arguments.done") {
		t.Errorf("both calls must open before either closes: %v", got.eventTypes)
	}
}

// Fragments without an index are tied together by call id, across text.
func TestIngressStream_FragmentAfterTextAndMissingIndex(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"tool_calls":[{"id":"call_a","type":"function","function":{"name":"ls","arguments":"{\"a\":"}}]}`, ""),
		chunk(`{"content":"and text"}`, ""),
		chunk(`{"tool_calls":[{"id":"call_a","function":{"arguments":"1}"}}]}`, "stop"),
	)
	got := decodeWithOpenAISDK(t, sse)
	if len(got.itemsDone) != 2 {
		t.Fatalf("items = %d %v", len(got.itemsDone), got.eventTypes)
	}
	call := got.itemsDone[1].AsFunctionCall()
	if call.CallID != "call_a" || call.Name != "ls" || call.Arguments != `{"a":1}` {
		t.Errorf("call item = %+v", call)
	}
}

func TestIngressStream_EmptyCompletionAndLength(t *testing.T) {
	got := decodeWithOpenAISDK(t, runIngress(t, NewIngressStreamTranslator("resp_1", "m", nil), chunk(`{"role":"assistant"}`, "stop")))
	if strings.Join(got.eventTypes, ",") != "response.created,response.in_progress,response.completed" || len(got.final.Output) != 0 {
		t.Errorf("empty completion: %v %+v", got.eventTypes, got.final)
	}
	// No frames at all: Finish alone still yields a well-formed response.
	got = decodeWithOpenAISDK(t, runIngress(t, NewIngressStreamTranslator("resp_1", "m", nil)))
	if got.final == nil || got.final.Status != "completed" {
		t.Errorf("bare finish: %+v", got.final)
	}
	got = decodeWithOpenAISDK(t, runIngress(t, NewIngressStreamTranslator("resp_1", "m", nil), chunk(`{"content":"cut"}`, "length")))
	if got.eventTypes[len(got.eventTypes)-1] != "response.incomplete" || got.final.Status != "incomplete" || got.final.IncompleteDetails.Reason != "max_output_tokens" {
		t.Errorf("length: %v %+v", got.eventTypes, got.final)
	}
}

func TestIngressStream_FailEndsStream(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	var buf bytes.Buffer
	b, _ := tr.Translate([]byte(chunk(`{"content":"par"}`, "")))
	buf.Write(b)
	buf.Write(tr.Fail("upstream died", "provider_error"))
	if late, _ := tr.Translate([]byte(chunk(`{"content":"tial"}`, ""))); len(late) != 0 {
		t.Error("nothing may follow the failure")
	}
	if fin, _ := tr.Finish(); len(fin) != 0 {
		t.Error("Finish after Fail must emit nothing")
	}
	if again := tr.Fail("x", ""); len(again) != 0 {
		t.Error("Fail twice must emit once")
	}
	got := decodeWithOpenAISDK(t, buf.Bytes())
	if got.failed == nil || got.failed.Status != "failed" || got.failed.Error.Message != "upstream died" || string(got.failed.Error.Code) != "provider_error" {
		t.Fatalf("failed = %+v", got.failed)
	}
	// The open message is closed first, so the partial text is on the snapshot.
	if got.failed.OutputText() != "par" {
		t.Errorf("partial text = %q", got.failed.OutputText())
	}
	if got.eventTypes[len(got.eventTypes)-1] != "response.failed" {
		t.Errorf("events = %v", got.eventTypes)
	}
}

func TestIngressStream_BadFrameIsReportedNotFatal(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	if _, err := tr.Translate([]byte(`{not json`)); err == nil || strings.Contains(err.Error(), "not json") {
		t.Fatalf("want a decode error that does not echo the frame, got %v", err)
	}
	got := decodeWithOpenAISDK(t, runIngress(t, tr, chunk(`{"content":"ok"}`, "stop")))
	if got.text != "ok" {
		t.Errorf("stream must survive a bad frame: %q", got.text)
	}
}

func TestIngressStream_ContentAfterFinishIgnoredUsageStillRead(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	got := decodeWithOpenAISDK(t, runIngress(t, tr,
		chunk(`{"content":"a"}`, "stop"),
		chunk(`{"content":"b"}`, ""),
		usageChunk,
	))
	if got.text != "a" || got.final.Usage.InputTokens != 10 {
		t.Errorf("text=%q usage=%+v", got.text, got.final.Usage)
	}
}

func TestBuildStreamFailure(t *testing.T) {
	sse := BuildStreamFailure("stalled", "", "resp_up", 7)
	got := decodeWithOpenAISDK(t, sse)
	if got.failed == nil || got.failed.Error.Message != "stalled" || string(got.failed.Error.Code) != "server_error" || got.failed.ID != "resp_up" {
		t.Fatalf("failed = %+v", got.failed)
	}
	if !strings.Contains(string(sse), `"sequence_number":7`) {
		t.Errorf("frame must continue the stream's sequence:\n%s", sse)
	}
}

// A call to a namespaced tool comes back as a function_call item naming the
// tool and its namespace separately, on every event that names it.
func TestIngressStream_NamespacedCallReversed(t *testing.T) {
	names := &RequestFacts{ToolNames: ToolNames{"mcp__fs__list": {Namespace: "mcp__fs", Name: "list"}}}
	tr := NewIngressStreamTranslator("resp_1", "m", names)
	sse := runIngress(t, tr,
		chunk(`{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"mcp__fs__list","arguments":"{}"}}]}`, "tool_calls"),
	)
	got := decodeWithOpenAISDK(t, sse)
	fc := got.itemsDone[0].AsFunctionCall()
	if fc.Name != "list" || fc.Namespace != "mcp__fs" || fc.CallID != "call_a" {
		t.Errorf("done item = %+v", fc)
	}
	if !strings.Contains(string(sse), `"name":"list"`) || strings.Contains(string(sse), `"name":"mcp__fs__list"`) {
		t.Errorf("flat name must not leak:\n%s", sse)
	}
	body, err := BuildResponse([]byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"mcp__fs__list","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), "resp_1", "m", names)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeResponse(t, body)
	if fc := r.Output[0].AsFunctionCall(); fc.Name != "list" || fc.Namespace != "mcp__fs" {
		t.Errorf("non-streaming item = %+v", fc)
	}
}

// Parallel calls whose argument fragments interleave each accumulate on their
// own item: both done events carry the full arguments.
func TestIngressStream_InterleavedParallelCalls(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"ls","arguments":"{\"p\":"}}]}`, ""),
		chunk(`{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"cat","arguments":"{\"f\":"}}]}`, ""),
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"a\"}"}}]}`, ""),
		chunk(`{"tool_calls":[{"index":1,"function":{"arguments":"\"b\"}"}}]}`, ""),
		chunk(`{}`, "tool_calls"),
	)
	got := decodeWithOpenAISDK(t, sse)
	if len(got.itemsDone) != 2 {
		t.Fatalf("items done = %d: %v", len(got.itemsDone), got.eventTypes)
	}
	a, b := got.itemsDone[0].AsFunctionCall(), got.itemsDone[1].AsFunctionCall()
	if a.CallID != "call_a" || a.Arguments != `{"p":"a"}` || b.CallID != "call_b" || b.Arguments != `{"f":"b"}` {
		t.Errorf("a=%+v b=%+v", a, b)
	}
	if len(got.final.Output) != 2 {
		t.Errorf("final output = %+v", got.final.Output)
	}
}

// A call whose fragments are separated by text stays open across it and
// closes exactly once, with the full arguments, so no duplicate call reaches
// the client.
func TestIngressStream_CallSpanningTextClosesOnce(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"ls","arguments":"{\"p\":"}}]}`, ""),
		chunk(`{"content":"text between"}`, ""),
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"a\"}"}}]}`, "stop"),
	)
	got := decodeWithOpenAISDK(t, sse)
	var calls []responses.ResponseFunctionToolCall
	for _, it := range got.itemsDone {
		if it.Type == "function_call" {
			calls = append(calls, it.AsFunctionCall())
		}
	}
	if len(calls) != 1 || calls[0].CallID != "call_a" || calls[0].Arguments != `{"p":"a"}` {
		t.Errorf("calls = %+v", calls)
	}
	if got.text != "text between" || len(got.final.Output) != 2 {
		t.Errorf("text=%q output=%d", got.text, len(got.final.Output))
	}
}

func TestIngressStream_ContentFilterIsIncomplete(t *testing.T) {
	got := decodeWithOpenAISDK(t, runIngress(t, NewIngressStreamTranslator("resp_1", "m", nil), chunk(`{"content":"x"}`, "content_filter")))
	if got.final.Status != "incomplete" || got.final.IncompleteDetails.Reason != "content_filter" {
		t.Errorf("final = %+v", got.final)
	}
}

// The Response object echoes the request's own members.
func TestIngressStream_EchoesRequestFacts(t *testing.T) {
	temp := 0.3
	off := false
	facts := &RequestFacts{Instructions: "be terse", Temperature: &temp, ParallelToolCalls: &off, ToolChoice: []byte(`"required"`), Metadata: []byte(`{"k":"v"}`), TextFormat: []byte(`{"type":"json_object"}`), Reasoning: &Reasoning{Effort: "low"}}
	got := decodeWithOpenAISDK(t, runIngress(t, NewIngressStreamTranslator("resp_1", "m", facts), chunk(`{"content":"x"}`, "stop")))
	r := got.final
	if r.Instructions.OfString != "be terse" || r.Temperature != 0.3 || r.ParallelToolCalls || r.ToolChoice.OfToolChoiceMode != "required" || r.Metadata["k"] != "v" || r.Text.Format.Type != "json_object" || r.Reasoning.Effort != "low" {
		t.Errorf("echo = instructions=%v temp=%v parallel=%v choice=%+v metadata=%v", r.Instructions, r.Temperature, r.ParallelToolCalls, r.ToolChoice, r.Metadata)
	}
}

// A refusal streams as the message's refusal part, as BuildResponse shapes
// it, beside any text.
func TestIngressStream_RefusalPart(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"content":"I "}`, ""),
		chunk(`{"refusal":"can"}`, ""),
		chunk(`{"refusal":"not"}`, "stop"),
	)
	got := decodeWithOpenAISDK(t, sse)
	msg := got.itemsDone[0].AsMessage()
	if len(msg.Content) != 2 || msg.Content[0].Text != "I " || msg.Content[1].Refusal != "cannot" {
		t.Errorf("content = %+v", msg.Content)
	}
	joined := strings.Join(got.eventTypes, ",")
	if strings.Count(joined, "response.refusal.delta") != 2 || strings.Count(joined, "response.refusal.done") != 1 || strings.Count(joined, "response.content_part.added") != 2 {
		t.Errorf("events = %v", got.eventTypes)
	}
	if !strings.Contains(string(sse), `"content_index":1,"delta":"can"`) {
		t.Errorf("refusal part index must follow the text part:\n%s", sse)
	}
}

// Parallel calls a provider streams without indexes are told apart by id.
func TestIngressStream_IndexlessParallelCallsByID(t *testing.T) {
	tr := NewIngressStreamTranslator("resp_1", "m", nil)
	sse := runIngress(t, tr,
		chunk(`{"tool_calls":[{"id":"call_a","type":"function","function":{"name":"ls","arguments":"{\"a\":"}}]}`, ""),
		chunk(`{"tool_calls":[{"id":"call_b","type":"function","function":{"name":"cat","arguments":"{\"b\":"}}]}`, ""),
		chunk(`{"tool_calls":[{"id":"call_a","function":{"arguments":"1}"}}]}`, ""),
		chunk(`{"tool_calls":[{"id":"call_b","function":{"arguments":"2}"}}]}`, "tool_calls"),
	)
	got := decodeWithOpenAISDK(t, sse)
	if len(got.itemsDone) != 2 {
		t.Fatalf("items = %d", len(got.itemsDone))
	}
	a, b := got.itemsDone[0].AsFunctionCall(), got.itemsDone[1].AsFunctionCall()
	if a.CallID != "call_a" || a.Arguments != `{"a":1}` || b.CallID != "call_b" || b.Arguments != `{"b":2}` {
		t.Errorf("a=%+v b=%+v", a, b)
	}
}
