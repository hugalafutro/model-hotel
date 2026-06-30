package anthropic

import (
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
}

func TestScanStreamUsage(t *testing.T) {
	// message_start carries input tokens.
	in, hasIn, _, _ := ScanStreamUsage([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":15,"output_tokens":0}}}`))
	if !hasIn || in != 15 {
		t.Errorf("message_start input = (%d,%v), want (15,true)", in, hasIn)
	}
	// message_delta carries cumulative output tokens.
	_, _, out, hasOut := ScanStreamUsage([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`))
	if !hasOut || out != 23 {
		t.Errorf("message_delta output = (%d,%v), want (23,true)", out, hasOut)
	}
	// content_block_delta carries no usage.
	_, hasIn2, _, hasOut2 := ScanStreamUsage([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`))
	if hasIn2 || hasOut2 {
		t.Errorf("content_block_delta should report no usage, got hasIn=%v hasOut=%v", hasIn2, hasOut2)
	}
}
