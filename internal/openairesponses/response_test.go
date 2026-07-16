package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustTranslateResp(t *testing.T, respBody, model string) map[string]any {
	t.Helper()
	out, err := TranslateResponsesToChat([]byte(respBody), model)
	if err != nil {
		t.Fatalf("TranslateResponsesToChat: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	return m
}

func firstChoice(t *testing.T, m map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	choices, _ := m["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("choices = %v", m["choices"])
	}
	choice := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	return choice, msg
}

// The full shape: reasoning summary -> reasoning_content, message text ->
// content, function_call -> tool_calls, finish_reason tool_calls, usage with
// reasoning + cached detail.
func TestTranslateResponses_ReasoningToolsUsage(t *testing.T) {
	body := `{
		"id": "resp_abc", "model": "gpt-5.6-sol", "status": "completed", "created_at": 1750000000,
		"output": [
			{"type": "reasoning", "id": "rs_1", "summary": [{"type": "summary_text", "text": "I should check the weather."}]},
			{"type": "message", "id": "msg_1", "role": "assistant", "content": [{"type": "output_text", "text": "Let me check."}]},
			{"type": "function_call", "id": "fc_1", "call_id": "call_9", "name": "get_weather", "arguments": "{\"city\":\"oslo\"}"}
		],
		"usage": {
			"input_tokens": 120, "output_tokens": 80, "total_tokens": 200,
			"input_tokens_details": {"cached_tokens": 100},
			"output_tokens_details": {"reasoning_tokens": 60}
		}
	}`
	m := mustTranslateResp(t, body, "hotel/gpt-5.6")

	if m["object"] != "chat.completion" {
		t.Errorf("object = %v", m["object"])
	}
	if id, _ := m["id"].(string); !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("id = %v, want chatcmpl- prefix", m["id"])
	}
	if m["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v", m["model"])
	}

	choice, msg := firstChoice(t, m)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", choice["finish_reason"])
	}
	if msg["content"] != "Let me check." {
		t.Errorf("content = %v", msg["content"])
	}
	if msg["reasoning_content"] != "I should check the weather." {
		t.Errorf("reasoning_content = %v", msg["reasoning_content"])
	}
	tcs, _ := msg["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %v", msg["tool_calls"])
	}
	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if tc["id"] != "call_9" || tc["type"] != "function" || fn["name"] != "get_weather" || fn["arguments"] != `{"city":"oslo"}` {
		t.Errorf("tool call = %v", tc)
	}

	usage, _ := m["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(120) || usage["completion_tokens"] != float64(80) || usage["total_tokens"] != float64(200) {
		t.Errorf("usage = %v", usage)
	}
	if d, _ := usage["completion_tokens_details"].(map[string]any); d["reasoning_tokens"] != float64(60) {
		t.Errorf("completion details = %v", usage["completion_tokens_details"])
	}
	if d, _ := usage["prompt_tokens_details"].(map[string]any); d["cached_tokens"] != float64(100) {
		t.Errorf("prompt details = %v", usage["prompt_tokens_details"])
	}
}

// Truncation by max_output_tokens maps to finish_reason "length"; a plain
// completed text answer maps to "stop" with null tool_calls.
func TestTranslateResponses_FinishReasons(t *testing.T) {
	m := mustTranslateResp(t, `{
		"id": "resp_1", "status": "incomplete", "incomplete_details": {"reason": "max_output_tokens"},
		"output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "truncat"}]}]
	}`, "m")
	choice, msg := firstChoice(t, m)
	if choice["finish_reason"] != "length" {
		t.Errorf("finish_reason = %v, want length", choice["finish_reason"])
	}
	if msg["content"] != "truncat" {
		t.Errorf("content = %v", msg["content"])
	}

	m = mustTranslateResp(t, `{
		"id": "resp_2", "status": "completed",
		"output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "hi"}]}]
	}`, "m")
	choice, msg = firstChoice(t, m)
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", choice["finish_reason"])
	}
	if _, ok := msg["tool_calls"]; ok {
		t.Errorf("tool_calls should be omitted: %v", msg)
	}
}

// Multiple reasoning summary parts join with a blank line; multiple message
// text parts concatenate; invalid tool arguments fall back to "{}".
func TestTranslateResponses_MultiPartAndBadArgs(t *testing.T) {
	m := mustTranslateResp(t, `{
		"id": "resp_3", "status": "completed",
		"output": [
			{"type": "reasoning", "summary": [{"type": "summary_text", "text": "part one"}, {"type": "summary_text", "text": "part two"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "a"}, {"type": "output_text", "text": "b"}]},
			{"type": "function_call", "id": "fc_1", "name": "f", "arguments": "not-json"}
		]
	}`, "m")
	_, msg := firstChoice(t, m)
	if msg["reasoning_content"] != "part one\n\npart two" {
		t.Errorf("reasoning_content = %q", msg["reasoning_content"])
	}
	if msg["content"] != "ab" {
		t.Errorf("content = %v", msg["content"])
	}
	tcs, _ := msg["tool_calls"].([]any)
	tc := tcs[0].(map[string]any)
	if tc["function"].(map[string]any)["arguments"] != "{}" {
		t.Errorf("bad arguments not defaulted: %v", tc)
	}
	// call_id absent -> falls back to item id.
	if tc["id"] != "fc_1" {
		t.Errorf("tool call id = %v, want fc_1 fallback", tc["id"])
	}
}

// A 200 body that is not a Responses object must error (so the proxy fails
// over) instead of translating into an empty completion.
func TestTranslateResponses_NotAResponsesBody(t *testing.T) {
	if _, err := TranslateResponsesToChat([]byte(`{"some":"thing"}`), "m"); err == nil {
		t.Error("want error for non-Responses body")
	}
	if _, err := TranslateResponsesToChat([]byte(`not json`), "m"); err == nil {
		t.Error("want error for invalid JSON")
	}
}
