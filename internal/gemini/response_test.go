package gemini

import (
	"encoding/json"
	"testing"
)

func decodeOAI(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("translated response is not valid JSON: %v\n%s", err, body)
	}
	return m
}

func TestBuildChatCompletion_TextAndUsage(t *testing.T) {
	// Shape captured live from Vertex express 2026-07-18 (thoughtsTokenCount is
	// real: totalTokenCount > prompt+candidates when the model thinks).
	body := []byte(`{
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "Hotel"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 8,
			"candidatesTokenCount": 1,
			"totalTokenCount": 31,
			"thoughtsTokenCount": 22
		},
		"modelVersion": "gemini-2.5-flash"
	}`)

	out, err := BuildChatCompletion(body, "chatcmpl-abc", "gemini-2.5-flash", 1752861431)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	m := decodeOAI(t, out)

	if m["id"] != "chatcmpl-abc" || m["object"] != "chat.completion" || m["model"] != "gemini-2.5-flash" {
		t.Errorf("envelope = id:%v object:%v model:%v", m["id"], m["object"], m["model"])
	}
	if m["created"] != float64(1752861431) {
		t.Errorf("created = %v", m["created"])
	}

	choice := m["choices"].([]any)[0].(map[string]any)
	if choice["index"] != float64(0) || choice["finish_reason"] != "stop" {
		t.Errorf("choice = %v", choice)
	}
	msg := choice["message"].(map[string]any)
	if msg["role"] != "assistant" || msg["content"] != "Hotel" {
		t.Errorf("message = %v", msg)
	}

	usage := m["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(8) {
		t.Errorf("prompt_tokens = %v", usage["prompt_tokens"])
	}
	// Thinking tokens are billed output: completion = candidates + thoughts.
	if usage["completion_tokens"] != float64(23) {
		t.Errorf("completion_tokens = %v, want 23", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(31) {
		t.Errorf("total_tokens = %v", usage["total_tokens"])
	}
	details := usage["completion_tokens_details"].(map[string]any)
	if details["reasoning_tokens"] != float64(22) {
		t.Errorf("reasoning_tokens = %v", details["reasoning_tokens"])
	}
}

func TestBuildChatCompletion_MultiPartTextSkipsThoughts(t *testing.T) {
	body := []byte(`{
		"candidates": [{
			"content": {"role": "model", "parts": [
				{"text": "secret plan", "thought": true},
				{"text": "Hello "},
				{"text": "world"}
			]},
			"finishReason": "MAX_TOKENS"
		}]
	}`)

	out, err := BuildChatCompletion(body, "id", "m", 0)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	choice := decodeOAI(t, out)["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "Hello world" {
		t.Errorf("content = %v, want thought part skipped and text joined", choice["message"])
	}
	if choice["finish_reason"] != "length" {
		t.Errorf("finish_reason = %v, want length", choice["finish_reason"])
	}
}

func TestBuildChatCompletion_FunctionCalls(t *testing.T) {
	// Args spread over lines the way Vertex pretty-prints them live; the
	// translated arguments string must come out compacted.
	body := []byte(`{
		"candidates": [{
			"content": {"role": "model", "parts": [
				{"functionCall": {"name": "get_weather", "args": {
					"city": "Oslo"
				}}}
			]},
			"finishReason": "STOP"
		}]
	}`)

	out, err := BuildChatCompletion(body, "id", "m", 0)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	choice := decodeOAI(t, out)["choices"].([]any)[0].(map[string]any)
	// A functionCall response reports tool_calls, not stop.
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	tc := msg["tool_calls"].([]any)[0].(map[string]any)
	if tc["type"] != "function" || tc["id"] == "" {
		t.Errorf("tool_call = %v", tc)
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("function name = %v", fn["name"])
	}
	if fn["arguments"] != `{"city":"Oslo"}` {
		t.Errorf("arguments = %q, want compacted {\"city\":\"Oslo\"}", fn["arguments"])
	}
}

func TestBuildChatCompletion_SafetyAndErrors(t *testing.T) {
	out, err := BuildChatCompletion([]byte(`{
		"candidates": [{"content": {"role": "model", "parts": []}, "finishReason": "SAFETY"}]
	}`), "id", "m", 0)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	choice := decodeOAI(t, out)["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "content_filter" {
		t.Errorf("finish_reason = %v, want content_filter", choice["finish_reason"])
	}

	if _, err := BuildChatCompletion([]byte(`{not json`), "id", "m", 0); err == nil {
		t.Error("expected error for invalid JSON")
	}
	// Prompt blocked: no candidates at all.
	if _, err := BuildChatCompletion([]byte(`{"promptFeedback": {"blockReason": "SAFETY"}}`), "id", "m", 0); err == nil {
		t.Error("expected error for zero candidates")
	}
}
