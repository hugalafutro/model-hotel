package anthropic

import (
	"encoding/json"
	"testing"
)

// decodeOAI unmarshals a translated OpenAI request body for assertions.
func decodeOAI(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("translated body is not valid JSON: %v\n%s", err, body)
	}
	return m
}

func TestTranslateRequest_SystemAndText(t *testing.T) {
	body := []byte(`{
		"model": "hotel/claude",
		"max_tokens": 100,
		"system": "You are helpful.",
		"temperature": 0.7,
		"stop_sequences": ["STOP"],
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)
	out, model, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if model != "hotel/claude" {
		t.Errorf("model = %q, want hotel/claude", model)
	}
	if stream {
		t.Errorf("stream = true, want false")
	}
	m := decodeOAI(t, out)
	msgs := m["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "You are helpful." {
		t.Errorf("system msg = %v", sys)
	}
	usr := msgs[1].(map[string]any)
	if usr["role"] != "user" || usr["content"] != "Hello" {
		t.Errorf("user msg = %v", usr)
	}
	if m["max_tokens"].(float64) != 100 {
		t.Errorf("max_tokens = %v, want 100", m["max_tokens"])
	}
	if m["temperature"].(float64) != 0.7 {
		t.Errorf("temperature = %v", m["temperature"])
	}
	stop := m["stop"].([]any)
	if len(stop) != 1 || stop[0] != "STOP" {
		t.Errorf("stop = %v", stop)
	}
}

func TestTranslateRequest_ImageBase64(t *testing.T) {
	body := []byte(`{
		"model": "p/m", "max_tokens": 50,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}}
			]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	parts := m["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content parts = %d, want 2", len(parts))
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("part type = %v", img["type"])
	}
	url := img["image_url"].(map[string]any)["url"].(string)
	if url != "data:image/png;base64,AAAA" {
		t.Errorf("image url = %q", url)
	}
}

func TestTranslateRequest_ToolUseAndResult(t *testing.T) {
	body := []byte(`{
		"model": "p/m", "max_tokens": 50,
		"tools": [{"name": "get_weather", "description": "weather", "input_schema": {"type":"object"}}],
		"tool_choice": {"type": "any"},
		"messages": [
			{"role": "user", "content": "weather in Paris?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Checking."},
				{"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"city": "Paris"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "call_1", "content": "sunny"}
			]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)

	// tools
	tools := m["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d", len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool name = %v", fn["name"])
	}
	if m["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v, want required", m["tool_choice"])
	}

	msgs := m["messages"].([]any)
	// user, assistant(tool_calls), tool
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3: %v", len(msgs), msgs)
	}
	asst := msgs[1].(map[string]any)
	if asst["role"] != "assistant" || asst["content"] != "Checking." {
		t.Errorf("assistant msg = %v", asst)
	}
	tcs := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %d", len(tcs))
	}
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("tool_call id = %v", tc["id"])
	}
	if args := tc["function"].(map[string]any)["arguments"].(string); args != `{"city": "Paris"}` && args != `{"city":"Paris"}` {
		t.Errorf("tool_call args = %q", args)
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "sunny" {
		t.Errorf("tool msg = %v", tool)
	}
}

func TestTranslateRequest_ToolChoiceTool(t *testing.T) {
	body := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"tool","name":"foo"},"messages":[{"role":"user","content":"x"}]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	tc := m["tool_choice"].(map[string]any)
	if tc["type"] != "function" {
		t.Errorf("tool_choice type = %v", tc["type"])
	}
	if tc["function"].(map[string]any)["name"] != "foo" {
		t.Errorf("tool_choice fn = %v", tc["function"])
	}
}

func TestTranslateRequest_StreamFlag(t *testing.T) {
	body := []byte(`{"model":"p/m","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"x"}]}`)
	_, _, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if !stream {
		t.Errorf("stream = false, want true")
	}
}

func TestTranslateRequest_MissingModel(t *testing.T) {
	_, _, _, err := TranslateRequest([]byte(`{"max_tokens":10,"messages":[]}`))
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}
