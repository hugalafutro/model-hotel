package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRewriteModel(t *testing.T) {
	body := []byte(`{"model":"hotel/claude","max_tokens":10,"system":"hi","messages":[{"role":"user","content":"x"}]}`)
	out := RewriteModel(body, "claude-haiku-4-5-20251001")
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["model"] != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %v, want rewritten", m["model"])
	}
	// Everything else preserved.
	if m["max_tokens"].(float64) != 10 || m["system"] != "hi" {
		t.Errorf("other fields altered: %v", m)
	}
	if msgs := m["messages"].([]any); len(msgs) != 1 {
		t.Errorf("messages altered: %v", m["messages"])
	}
}

func TestRewriteModel_InvalidBodyUnchanged(t *testing.T) {
	body := []byte(`not json`)
	if out := RewriteModel(body, "x"); string(out) != "not json" {
		t.Errorf("invalid body should be returned unchanged, got %q", out)
	}
}

func TestParseResponseUsage(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":42,"output_tokens":7}}`)
	in, out := ParseResponseUsage(body)
	if in != 42 || out != 7 {
		t.Errorf("usage = (%d,%d), want (42,7)", in, out)
	}
	// Invalid body yields zeros.
	if in, out := ParseResponseUsage([]byte(`not json`)); in != 0 || out != 0 {
		t.Errorf("invalid usage = (%d,%d), want (0,0)", in, out)
	}
}

// Anthropic's cache counts are disjoint additions to input_tokens, not a
// breakdown of it, so a warm-cache request reports a tiny input_tokens beside a
// huge cache_read_input_tokens. Metering the bare field would under-report the
// prompt by the whole cached figure.
func TestParseResponseUsage_SumsCachedInput(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":4,"output_tokens":50,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}`)
	in, out := ParseResponseUsage(body)
	if in != 20034 {
		t.Errorf("prompt tokens = %d, want 20034 (4 + 20000 + 30)", in)
	}
	if out != 50 {
		t.Errorf("completion tokens = %d, want 50", out)
	}
}

// ResponseCarriesContent decides whether a native 200 counts as the model
// answering, which is what clears its gone-strike streak in the proxy. The
// distinction that matters is a nonempty body carrying an empty content array:
// an aggregator in front of a retired model returns exactly that between its
// refusals, and crediting it would stop the streak ever reaching three
// consecutive strikes.
func TestResponseCarriesContent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"text block", `{"id":"m","type":"message","content":[{"type":"text","text":"hi"}]}`, true},
		// Any block type is the model producing something; the vocabulary is
		// Anthropic's to extend, so the blocks are deliberately not decoded.
		{"tool_use block", `{"id":"m","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"f"}]}`, true},
		{"unknown block type still counts", `{"id":"m","type":"message","content":[{"type":"something_new"}]}`, true},
		{"empty content array", `{"id":"m","type":"message","content":[]}`, false},
		{"content absent", `{"id":"m","type":"message","usage":{"output_tokens":3}}`, false},
		{"content null", `{"id":"m","type":"message","content":null}`, false},
		{"not json", `not json`, false},
		{"empty body", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResponseCarriesContent([]byte(tt.body)); got != tt.want {
				t.Errorf("ResponseCarriesContent(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestStreamTranslator_ToolWithoutID_AndIdempotentFinish(t *testing.T) {
	tr := NewStreamTranslator("msg_t", "m")
	// A tool-call fragment with no id forces id synthesis; arguments stream as
	// input_json_delta.
	out, err := tr.Translate(OAStreamChunk{Choices: []OAStreamChoice{{
		Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{{
			Index: 0, Function: OAFunctionDelta{Name: "f", Arguments: `{"a":1}`},
		}}},
	}}})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !bytes.Contains(out, []byte("toolu_")) {
		t.Errorf("expected synthesized toolu_ id in output:\n%s", out)
	}
	if !bytes.Contains(out, []byte("input_json_delta")) {
		t.Errorf("expected input_json_delta:\n%s", out)
	}
	// Finish is idempotent: the second call emits nothing.
	if _, err := tr.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	again, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish (2nd): %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second Finish should emit nothing, got %s", again)
	}
}

func TestInspectStreamEvent(t *testing.T) {
	// message_start carries input tokens.
	if ev := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":15,"output_tokens":0}}}`)); ev.Type != "message_start" || !ev.HasInput || ev.InputTokens != 15 {
		t.Errorf("message_start = %+v, want type=message_start input=15", ev)
	}
	// message_delta carries cumulative output tokens.
	if ev := InspectStreamEvent([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`)); ev.Type != "message_delta" || !ev.HasOutput || ev.OutputTokens != 23 {
		t.Errorf("message_delta = %+v, want type=message_delta output=23", ev)
	}
	// content_block_delta carries no usage.
	if ev := InspectStreamEvent([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)); ev.HasInput || ev.HasOutput {
		t.Errorf("content_block_delta should report no usage, got %+v", ev)
	}
	// message_stop is the terminal marker.
	if ev := InspectStreamEvent([]byte(`{"type":"message_stop"}`)); ev.Type != "message_stop" {
		t.Errorf("message_stop type = %q, want message_stop", ev.Type)
	}
	// error event surfaces the message.
	if ev := InspectStreamEvent([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)); ev.Type != "error" || ev.ErrorMessage != "Overloaded" {
		t.Errorf("error = %+v, want type=error message=Overloaded", ev)
	}
	// garbage parses to a zero value.
	if ev := InspectStreamEvent([]byte(`not json`)); ev.Type != "" {
		t.Errorf("garbage = %+v, want zero StreamEvent", ev)
	}
}

// The streamed message_start reports the same summed prompt size the
// non-streaming path does, so the two native paths meter a cached prompt alike.
func TestInspectStreamEvent_SumsCachedInput(t *testing.T) {
	ev := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":1,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}}`))
	if !ev.HasInput || ev.InputTokens != 20034 {
		t.Errorf("input tokens = %d (has=%v), want 20034 (4 + 20000 + 30)", ev.InputTokens, ev.HasInput)
	}
	if !ev.HasOutput || ev.OutputTokens != 1 {
		t.Errorf("output tokens = %d (has=%v), want 1", ev.OutputTokens, ev.HasOutput)
	}
}
