package openairesponses

import (
	"strings"
	"testing"
)

// An upstream can emit a delta event carrying nothing. Forwarding it would put
// an empty content/reasoning fragment on the wire for every heartbeat-like
// event, which some clients render as a stray token. Nothing goes out.
func TestStream_EmptyDeltasEmitNothing(t *testing.T) {
	for _, evType := range []string{
		"response.output_text.delta",
		"response.reasoning_summary_text.delta",
		"response.function_call_arguments.delta",
	} {
		t.Run(evType, func(t *testing.T) {
			tr := NewStreamTranslator("hotel/gpt-5.6")
			out := feed(t, tr, `{"type":"`+evType+`","delta":"","output_index":0}`)
			if len(out) != 0 {
				t.Fatalf("empty delta produced %q", out)
			}
		})
	}
}

// Each reasoning summary part is a separate paragraph of the model's thinking.
// The first opens the reasoning stream and needs no separator; every later one
// gets a blank line first, or the parts run together into one wall of text.
func TestStream_SecondReasoningPartOpensWithABlankLine(t *testing.T) {
	tr := NewStreamTranslator("hotel/gpt-5.6")
	out := feed(t, tr,
		`{"type":"response.reasoning_summary_part.added","output_index":0}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"first"}`,
	)
	if strings.Contains(string(out), `\n\n`) {
		t.Fatalf("the first summary part emitted a separator: %s", out)
	}

	out = feed(t, tr, `{"type":"response.reasoning_summary_part.added","output_index":0}`)
	chunks, _ := collectChunks(t, out)
	if len(chunks) != 1 {
		t.Fatalf("second summary part produced %d chunks, want 1: %s", len(chunks), out)
	}
	if got := delta(t, chunks[0])["reasoning_content"]; got != "\n\n" {
		t.Errorf("separator = %q, want a blank line", got)
	}
}

// output_item.added announces a tool call. Only function_call items matter;
// a reasoning or message item announced the same way must not open a
// tool_calls entry the client then waits for arguments on.
func TestStream_OutputItemAddedOnlyOpensFunctionCalls(t *testing.T) {
	tr := NewStreamTranslator("hotel/gpt-5.6")
	out := feed(t, tr,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
		`{"type":"response.output_item.added","output_index":1}`,
	)
	if len(out) != 0 {
		t.Fatalf("non-function items opened a tool call: %s", out)
	}
}

// The call id is what the client echoes back on the tool result, so it has to
// be the Responses call_id. Some items arrive with only the item id; falling
// back to it keeps the round trip working instead of sending an empty id.
func TestStream_ToolCallIDFallsBackToTheItemID(t *testing.T) {
	tr := NewStreamTranslator("hotel/gpt-5.6")
	out := feed(t, tr,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_abc","name":"get_weather"}}`,
	)
	chunks, _ := collectChunks(t, out)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %s", len(chunks), out)
	}
	calls, _ := delta(t, chunks[0])["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %v", delta(t, chunks[0])["tool_calls"])
	}
	call := calls[0].(map[string]any)
	if call["id"] != "fc_abc" {
		t.Errorf("id = %v, want the item id as the fallback", call["id"])
	}

	// call_id wins when both are present: it is the identifier the Responses
	// API expects back on the function_call_output.
	tr = NewStreamTranslator("hotel/gpt-5.6")
	out = feed(t, tr,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_abc","call_id":"call_xyz","name":"get_weather"}}`,
	)
	chunks, _ = collectChunks(t, out)
	calls, _ = delta(t, chunks[0])["tool_calls"].([]any)
	if got := calls[0].(map[string]any)["id"]; got != "call_xyz" {
		t.Errorf("id = %v, want the call_id", got)
	}
}

// A top-level error event ends the stream as an OpenAI error frame, which is
// what the streaming pipeline records as the failure cause. An error with no
// message still has to produce a frame: a silent [DONE] would look to the
// client like a successful empty completion.
func TestStream_TopLevelErrorEvent(t *testing.T) {
	for _, tc := range []struct {
		name, event, wantMsg string
	}{
		{"with a message", `{"type":"error","code":"rate_limit","message":"slow down"}`, "slow down"},
		{"without a message", `{"type":"error","code":"rate_limit"}`, "upstream stream error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewStreamTranslator("hotel/gpt-5.6")
			out := feed(t, tr, tc.event)
			chunks, sawDone := collectChunks(t, out)
			if !sawDone {
				t.Fatal("error frame did not end the stream with [DONE]")
			}
			if len(chunks) != 1 {
				t.Fatalf("got %d chunks, want 1 error frame: %s", len(chunks), out)
			}
			errObj, ok := chunks[0]["error"].(map[string]any)
			if !ok {
				t.Fatalf("chunk is not an error frame: %v", chunks[0])
			}
			if errObj["message"] != tc.wantMsg {
				t.Errorf("message = %v, want %q", errObj["message"], tc.wantMsg)
			}
			if errObj["type"] != "server_error" {
				t.Errorf("type = %v, want server_error", errObj["type"])
			}

			// The stream is over: later events are ignored rather than
			// appended after [DONE].
			if extra := feed(t, tr, `{"type":"response.output_text.delta","delta":"late"}`); len(extra) != 0 {
				t.Errorf("event after the error produced %q", extra)
			}
		})
	}
}

// A completion that produced no deltas at all still owes the client a
// well-formed stream: the finish chunk carries the assistant role, because
// nothing before it did.
func TestStream_EmptyCompletionStillSendsTheRole(t *testing.T) {
	tr := NewStreamTranslator("hotel/gpt-5.6")
	out := feed(t, tr, `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`)
	chunks, sawDone := collectChunks(t, out)
	if !sawDone {
		t.Fatal("no [DONE] sentinel")
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %s", len(chunks), out)
	}
	if got := delta(t, chunks[0])["role"]; got != "assistant" {
		t.Errorf("role = %v, want assistant on the only chunk", got)
	}

	// When content already streamed, the finish chunk does not repeat the
	// role: a second role field mid-stream confuses accumulating clients.
	tr = NewStreamTranslator("hotel/gpt-5.6")
	out = feed(t, tr,
		`{"type":"response.output_text.delta","delta":"hi"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
	)
	chunks, _ = collectChunks(t, out)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %s", len(chunks), out)
	}
	if _, present := delta(t, chunks[1])["role"]; present {
		t.Errorf("finish chunk repeated the role: %v", delta(t, chunks[1]))
	}
}
