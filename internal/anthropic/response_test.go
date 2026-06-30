package anthropic

import (
	"encoding/json"
	"testing"
)

func TestBuildMessageResponse_Text(t *testing.T) {
	oai := []byte(`{
		"id": "chatcmpl-1", "model": "upstream-model",
		"choices": [{"message": {"role": "assistant", "content": "Hi there"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 4}
	}`)
	out, err := BuildMessageResponse(oai, "msg_1", "hotel/claude")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["id"] != "msg_1" || m["type"] != "message" || m["role"] != "assistant" {
		t.Errorf("envelope = %v", m)
	}
	if m["model"] != "hotel/claude" {
		t.Errorf("model = %v, want echoed request model", m["model"])
	}
	content := m["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "Hi there" {
		t.Errorf("content = %v", content)
	}
	if m["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", m["stop_reason"])
	}
	usage := m["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 12 || usage["output_tokens"].(float64) != 4 {
		t.Errorf("usage = %v", usage)
	}
}

func TestBuildMessageResponse_ToolUse(t *testing.T) {
	oai := []byte(`{
		"choices": [{"message": {"role":"assistant","content":"","tool_calls":[
			{"id":"call_9","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}
		]}, "finish_reason": "tool_calls"}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 7}
	}`)
	out, err := BuildMessageResponse(oai, "msg_2", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", m["stop_reason"])
	}
	content := m["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content len = %d, want 1 (empty text dropped)", len(content))
	}
	tu := content[0].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "call_9" || tu["name"] != "lookup" {
		t.Errorf("tool_use block = %v", tu)
	}
	input := tu["input"].(map[string]any)
	if input["q"] != "x" {
		t.Errorf("tool_use input = %v", input)
	}
}

func TestBuildErrorResponse_StatusMapping(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{400, "invalid_request_error"},
		{401, "authentication_error"},
		{403, "permission_error"},
		{404, "not_found_error"},
		{429, "rate_limit_error"},
		{500, "api_error"},
		{502, "api_error"},
		{503, "overloaded_error"},
	}
	for _, c := range cases {
		out := BuildErrorResponse([]byte(`{"error":{"message":"boom"}}`), c.status)
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("status %d: invalid output: %v", c.status, err)
		}
		if m["type"] != "error" {
			t.Errorf("status %d: type = %v", c.status, m["type"])
		}
		e := m["error"].(map[string]any)
		if e["type"] != c.want {
			t.Errorf("status %d: error type = %v, want %v", c.status, e["type"], c.want)
		}
		if e["message"] != "boom" {
			t.Errorf("status %d: message = %v, want boom (from OpenAI envelope)", c.status, e["message"])
		}
	}
}

// The proxy's WriteOpenAIError emits "code" as an int; the message must still be
// extracted (not leaked as the raw JSON envelope). Regression guard.
func TestBuildErrorResponse_NumericCode(t *testing.T) {
	out := BuildErrorResponse([]byte(`{"error":{"code":504,"message":"request timed out","type":"server_error"}}`), 504)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	e := m["error"].(map[string]any)
	if e["message"] != "request timed out" {
		t.Errorf("message = %v, want extracted \"request timed out\"", e["message"])
	}
	if e["type"] != "api_error" {
		t.Errorf("type = %v, want api_error (504)", e["type"])
	}
}

func TestBuildErrorResponse_RawBodyFallback(t *testing.T) {
	out := BuildErrorResponse([]byte(`not json`), 500)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	e := m["error"].(map[string]any)
	if e["message"] != "not json" {
		t.Errorf("message = %v, want raw body fallback", e["message"])
	}
}
