package openairesponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// collectChunks parses "data:" SSE frames out of translated bytes.
func collectChunks(t *testing.T, raw []byte) (chunks []map[string]any, sawDone bool) {
	t.Helper()
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatalf("chunk not valid JSON: %v\n%s", err, payload)
		}
		chunks = append(chunks, m)
	}
	return chunks, sawDone
}

func delta(t *testing.T, chunk map[string]any) map[string]any {
	t.Helper()
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	d, _ := choices[0].(map[string]any)["delta"].(map[string]any)
	return d
}

func feed(t *testing.T, tr *StreamTranslator, events ...string) []byte {
	t.Helper()
	var out []byte
	for _, ev := range events {
		b, err := tr.TranslateEvent([]byte(ev))
		if err != nil {
			t.Fatalf("TranslateEvent(%s): %v", ev, err)
		}
		out = append(out, b...)
	}
	return out
}

// Reasoning summary streams as reasoning_content, then text as content, then
// the terminal event emits finish_reason, a usage chunk and [DONE]. The first
// emitted delta carries the assistant role.
func TestStream_ReasoningThenText(t *testing.T) {
	tr := NewStreamTranslator("hotel/gpt-5.6")
	out := feed(t, tr,
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.reasoning_summary_part.added","output_index":0}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"thinking..."}`,
		`{"type":"response.output_text.delta","delta":"Hel"}`,
		`{"type":"response.output_text.delta","delta":"lo"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"output_tokens_details":{"reasoning_tokens":3}}}}`,
	)
	chunks, sawDone := collectChunks(t, out)
	if !sawDone {
		t.Fatal("no [DONE] sentinel")
	}
	// reasoning delta + 2 content deltas + finish chunk + usage chunk.
	if len(chunks) != 5 {
		t.Fatalf("got %d chunks, want 5:\n%s", len(chunks), out)
	}
	if d := delta(t, chunks[0]); d["role"] != "assistant" || d["reasoning_content"] != "thinking..." {
		t.Errorf("first chunk = %v, want role+reasoning_content", d)
	}
	if d := delta(t, chunks[1]); d["content"] != "Hel" {
		t.Errorf("chunk[1] = %v", d)
	}
	if d := delta(t, chunks[2]); d["content"] != "lo" || d["role"] != nil {
		t.Errorf("chunk[2] = %v (role must only be on first)", d)
	}
	finish := chunks[3]["choices"].([]any)[0].(map[string]any)
	if finish["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", finish["finish_reason"])
	}
	usage, _ := chunks[4]["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(10) || usage["completion_tokens"] != float64(5) {
		t.Errorf("usage chunk = %v", chunks[4])
	}
	if d, _ := usage["completion_tokens_details"].(map[string]any); d["reasoning_tokens"] != float64(3) {
		t.Errorf("reasoning detail = %v", usage)
	}
	if chunks[0]["object"] != "chat.completion.chunk" || chunks[0]["model"] != "hotel/gpt-5.6" {
		t.Errorf("chunk envelope = %v", chunks[0])
	}
}

// Two parallel tool calls keep separate, stable indexes; the terminal event
// maps to finish_reason tool_calls.
func TestStream_ParallelToolCalls(t *testing.T) {
	tr := NewStreamTranslator("m")
	out := feed(t, tr,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_a","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":"}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_b","name":"get_time"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{}"}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"\"oslo\"}"}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	)
	chunks, sawDone := collectChunks(t, out)
	if !sawDone {
		t.Fatal("no [DONE]")
	}
	type frag struct {
		idx  int
		id   string
		name string
		args string
	}
	var frags []frag
	for _, c := range chunks {
		d := delta(t, c)
		if d == nil {
			continue
		}
		tcs, _ := d["tool_calls"].([]any)
		for _, raw := range tcs {
			tc := raw.(map[string]any)
			fn, _ := tc["function"].(map[string]any)
			f := frag{idx: int(tc["index"].(float64))}
			f.id, _ = tc["id"].(string)
			f.name, _ = fn["name"].(string)
			f.args, _ = fn["arguments"].(string)
			frags = append(frags, f)
		}
	}
	want := []frag{
		{0, "call_a", "get_weather", ""},
		{0, "", "", `{"city":`},
		{1, "call_b", "get_time", ""},
		{1, "", "", "{}"},
		{0, "", "", `"oslo"}`},
	}
	if len(frags) != len(want) {
		t.Fatalf("got %d tool fragments, want %d: %v", len(frags), len(want), frags)
	}
	for i, w := range want {
		if frags[i] != w {
			t.Errorf("fragment[%d] = %v, want %v", i, frags[i], w)
		}
	}
	finish := chunks[len(chunks)-1]["choices"].([]any)[0].(map[string]any)
	if finish["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", finish["finish_reason"])
	}
}

// Unknown event types are ignored; incomplete/max_output_tokens maps to
// length; events after the terminal one are dropped.
func TestStream_UnknownEventsAndTruncation(t *testing.T) {
	tr := NewStreamTranslator("m")
	out := feed(t, tr,
		`{"type":"response.some_future_event","delta":"x"}`,
		`{"type":"response.output_text.delta","delta":"hi"}`,
		`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
		`{"type":"response.output_text.delta","delta":"late"}`,
	)
	chunks, sawDone := collectChunks(t, out)
	if !sawDone {
		t.Fatal("no [DONE]")
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (content + finish):\n%s", len(chunks), out)
	}
	finish := chunks[1]["choices"].([]any)[0].(map[string]any)
	if finish["finish_reason"] != "length" {
		t.Errorf("finish_reason = %v, want length", finish["finish_reason"])
	}
	if !tr.Finished() {
		t.Error("translator should report finished")
	}
}

// A failed response surfaces as an OpenAI-style error frame so the streaming
// pipeline records the provider's message, then the stream ends.
func TestStream_FailedResponse(t *testing.T) {
	tr := NewStreamTranslator("m")
	out := feed(t, tr,
		`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"boom"}}}`,
	)
	if !strings.Contains(string(out), `"error"`) || !strings.Contains(string(out), "boom") {
		t.Errorf("error frame missing: %s", out)
	}
	if !strings.Contains(string(out), "[DONE]") {
		t.Errorf("[DONE] missing after error: %s", out)
	}
}

// StreamAdapter end-to-end: Responses SSE bytes in (fragmented across reads,
// with event: lines and CRLF), chat-completions SSE out.
func TestStreamAdapter_EndToEnd(t *testing.T) {
	sse := "event: response.created\r\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\r\n" +
		"\r\n" +
		"event: response.output_text.delta\r\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\r\n" +
		"\r\n" +
		"event: response.completed\r\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\r\n" +
		"\r\n"
	// oneByteReader forces the adapter to reassemble lines across reads.
	adapter := NewStreamAdapter(io.NopCloser(iotest{r: strings.NewReader(sse)}), "m")
	out, err := io.ReadAll(adapter)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	chunks, sawDone := collectChunks(t, out)
	if !sawDone {
		t.Fatalf("no [DONE]:\n%s", out)
	}
	if len(chunks) != 3 { // content + finish + usage
		t.Fatalf("got %d chunks, want 3:\n%s", len(chunks), out)
	}
	if d := delta(t, chunks[0]); d["content"] != "Hello" {
		t.Errorf("chunk[0] = %v", d)
	}
	if err := adapter.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// iotest yields one byte per Read to exercise line reassembly.
type iotest struct{ r io.Reader }

func (o iotest) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return o.r.Read(p)
}
