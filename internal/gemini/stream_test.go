package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// parseFrames splits translated SSE bytes into decoded chunk payloads,
// returning the raw data strings (so [DONE] can be asserted) alongside.
func parseFrames(t *testing.T, sse []byte) []string {
	t.Helper()
	var payloads []string
	for frame := range strings.SplitSeq(string(sse), "\n\n") {
		if frame == "" {
			continue
		}
		data, ok := strings.CutPrefix(frame, "data: ")
		if !ok {
			t.Fatalf("frame missing data: prefix: %q", frame)
		}
		payloads = append(payloads, data)
	}
	return payloads
}

func decodeChunk(t *testing.T, payload string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("chunk is not valid JSON: %v\n%s", err, payload)
	}
	return m
}

func chunkDelta(t *testing.T, payload string) map[string]any {
	t.Helper()
	m := decodeChunk(t, payload)
	return m["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
}

func TestStreamTranslator_TextDeltas(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-s", "gemini-2.5-flash", 1752861431)

	out1, err := tr.Translate([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	frames := parseFrames(t, out1)
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	m := decodeChunk(t, frames[0])
	if m["id"] != "chatcmpl-s" || m["object"] != "chat.completion.chunk" || m["model"] != "gemini-2.5-flash" || m["created"] != float64(1752861431) {
		t.Errorf("envelope = %v", m)
	}
	choice := m["choices"].([]any)[0].(map[string]any)
	if choice["index"] != float64(0) || choice["finish_reason"] != nil {
		t.Errorf("choice = %v", choice)
	}
	// First content chunk carries the assistant role.
	delta := choice["delta"].(map[string]any)
	if delta["role"] != "assistant" || delta["content"] != "Hel" {
		t.Errorf("delta = %v", delta)
	}

	out2, err := tr.Translate([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}]}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	delta2 := chunkDelta(t, parseFrames(t, out2)[0])
	if _, ok := delta2["role"]; ok {
		t.Error("role repeated on second chunk")
	}
	if delta2["content"] != "lo" {
		t.Errorf("delta2 = %v", delta2)
	}
}

func TestStreamTranslator_FinishWithUsage(t *testing.T) {
	tr := NewStreamTranslator("id", "m", 0)

	// Final Gemini chunk shape captured live 2026-07-18: finishReason and
	// usageMetadata (incl. thoughtsTokenCount) arrive on the last data line.
	out, err := tr.Translate([]byte(`{
		"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":13,"candidatesTokenCount":9,"totalTokenCount":48,"thoughtsTokenCount":26}
	}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got := chunkDelta(t, parseFrames(t, out)[0])["content"]; got != "Hi" {
		t.Errorf("content = %v", got)
	}

	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	frames := parseFrames(t, fin)
	if len(frames) != 2 {
		t.Fatalf("finish frames = %d, want terminal chunk + [DONE]", len(frames))
	}
	if frames[1] != "[DONE]" {
		t.Errorf("last frame = %q, want [DONE]", frames[1])
	}
	m := decodeChunk(t, frames[0])
	choice := m["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if len(choice["delta"].(map[string]any)) != 0 {
		t.Errorf("terminal delta = %v, want empty", choice["delta"])
	}
	usage := m["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(13) || usage["completion_tokens"] != float64(35) || usage["total_tokens"] != float64(48) {
		t.Errorf("usage = %v", usage)
	}
	if usage["completion_tokens_details"].(map[string]any)["reasoning_tokens"] != float64(26) {
		t.Errorf("usage details = %v", usage)
	}

	// Finish is idempotent.
	again, err := tr.Finish()
	if err != nil || len(again) != 0 {
		t.Errorf("second Finish = %q, %v", again, err)
	}
}

func TestStreamTranslator_FunctionCalls(t *testing.T) {
	tr := NewStreamTranslator("sid", "m", 0)

	out, err := tr.Translate([]byte(`{"candidates":[{"content":{"role":"model","parts":[
		{"functionCall":{"name":"get_weather","args":{
			"city": "Oslo"
		}}},
		{"functionCall":{"name":"get_time","args":{}}}
	]},"finishReason":"STOP"}]}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	delta := chunkDelta(t, parseFrames(t, out)[0])
	calls := delta["tool_calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("tool_calls = %v", calls)
	}
	first := calls[0].(map[string]any)
	if first["index"] != float64(0) || first["type"] != "function" || first["id"] == "" {
		t.Errorf("tool_calls[0] = %v", first)
	}
	fn := first["function"].(map[string]any)
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"Oslo"}` {
		t.Errorf("function = %v, want compacted args", fn)
	}
	second := calls[1].(map[string]any)
	if second["index"] != float64(1) || second["id"] == first["id"] {
		t.Errorf("tool_calls[1] = %v", second)
	}

	// Tool calls flip the terminal finish_reason.
	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	choice := decodeChunk(t, parseFrames(t, fin)[0])["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", choice["finish_reason"])
	}
}

func TestStreamTranslator_ThoughtAndEmptyChunks(t *testing.T) {
	tr := NewStreamTranslator("id", "m", 0)

	// Thought parts and empty text produce no client-visible frames.
	out, err := tr.Translate([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"internal","thought":true},{"text":""}]}}]}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("thought chunk output = %q, want none", out)
	}

	// Usage-only chunk (no candidates): silent, but usage recorded for Finish.
	out, err = tr.Translate([]byte(`{"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`))
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("usage chunk output = %q, want none", out)
	}

	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	frames := parseFrames(t, fin)
	m := decodeChunk(t, frames[0])
	// Never-started stream still emits a well-formed terminal chunk with the
	// assistant role and recorded usage.
	choice := m["choices"].([]any)[0].(map[string]any)
	if choice["delta"].(map[string]any)["role"] != "assistant" {
		t.Errorf("terminal delta = %v, want role for never-started stream", choice["delta"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop default", choice["finish_reason"])
	}
	if m["usage"].(map[string]any)["prompt_tokens"] != float64(5) {
		t.Errorf("usage = %v", m["usage"])
	}
}

func TestStreamTranslator_InvalidChunk(t *testing.T) {
	tr := NewStreamTranslator("id", "m", 0)
	if _, err := tr.Translate([]byte(`{not json`)); err == nil {
		t.Error("expected error for invalid chunk JSON")
	}
}
