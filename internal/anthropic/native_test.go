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
