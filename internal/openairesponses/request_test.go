package openairesponses

import (
	"encoding/json"
	"testing"
)

// mustTranslate runs TranslateChatToResponses and decodes the result into a
// generic map for assertions.
func mustTranslate(t *testing.T, chatBody, model string) map[string]any {
	t.Helper()
	out, err := TranslateChatToResponses([]byte(chatBody), model)
	if err != nil {
		t.Fatalf("TranslateChatToResponses: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	return m
}

func inputItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["input"].([]any)
	if !ok {
		t.Fatalf("input missing or not an array: %v", m["input"])
	}
	items := make([]map[string]any, len(raw))
	for i, it := range raw {
		items[i], ok = it.(map[string]any)
		if !ok {
			t.Fatalf("input[%d] not an object: %v", i, it)
		}
	}
	return items
}

// The full transcript shape: system -> instructions, user/assistant text ->
// message items, assistant tool_calls -> function_call items, tool results ->
// function_call_output items, in order.
func TestTranslateChat_TranscriptWithTools(t *testing.T) {
	body := `{
		"model": "hotel/gpt-5.6",
		"messages": [
			{"role": "system", "content": "be terse"},
			{"role": "user", "content": "weather in oslo?"},
			{"role": "assistant", "content": "checking", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"oslo\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "12C rain"},
			{"role": "user", "content": "and bergen?"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "looks up weather", "parameters": {"type": "object"}}}],
		"reasoning_effort": "high",
		"max_completion_tokens": 500,
		"stream": true
	}`
	m := mustTranslate(t, body, "gpt-5.6-sol")

	if m["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v, want resolved gpt-5.6-sol", m["model"])
	}
	if m["instructions"] != "be terse" {
		t.Errorf("instructions = %v", m["instructions"])
	}
	if m["max_output_tokens"] != float64(500) {
		t.Errorf("max_output_tokens = %v, want 500", m["max_output_tokens"])
	}
	if m["stream"] != true {
		t.Error("stream flag lost")
	}
	if store, present := m["store"]; !present || store != false {
		t.Errorf("store = %v (present=%v), must be explicit false", store, present)
	}

	items := inputItems(t, m)
	wantTypes := []string{"message", "message", "function_call", "function_call_output", "message"}
	if len(items) != len(wantTypes) {
		t.Fatalf("got %d input items, want %d: %v", len(items), len(wantTypes), items)
	}
	for i, want := range wantTypes {
		if items[i]["type"] != want {
			t.Errorf("input[%d].type = %v, want %s", i, items[i]["type"], want)
		}
	}
	if items[2]["call_id"] != "call_1" || items[2]["name"] != "get_weather" {
		t.Errorf("function_call item wrong: %v", items[2])
	}
	if items[3]["call_id"] != "call_1" || items[3]["output"] != "12C rain" {
		t.Errorf("function_call_output item wrong: %v", items[3])
	}

	// Tools flattened (no nested "function" object).
	tools, _ := m["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", m["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "get_weather" || tool["description"] != "looks up weather" {
		t.Errorf("tool not flattened: %v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Errorf("tool still nests function object: %v", tool)
	}

	// reasoning_effort -> reasoning{effort, summary:auto}.
	reasoning, _ := m["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Errorf("reasoning = %v, want effort=high summary=auto", m["reasoning"])
	}
}

// Image parts map to input_image; text parts to input_text.
func TestTranslateChat_ImageContent(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":[
		{"type":"text","text":"what is this?"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,AAA"}}
	]}]}`
	m := mustTranslate(t, body, "m")
	items := inputItems(t, m)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	content := items[0]["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %v", content)
	}
	img := content[1].(map[string]any)
	if img["type"] != "input_image" || img["image_url"] != "data:image/png;base64,AAA" {
		t.Errorf("image part = %v", img)
	}
}

// Absent reasoning_effort still requests a summary (these models reason by
// default); explicit "none" keeps reasoning off and asks for no summary.
func TestTranslateChat_ReasoningDefaults(t *testing.T) {
	m := mustTranslate(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, "m")
	reasoning, _ := m["reasoning"].(map[string]any)
	if reasoning["summary"] != "auto" {
		t.Errorf("reasoning = %v, want summary=auto", m["reasoning"])
	}
	if _, hasEffort := reasoning["effort"]; hasEffort {
		t.Errorf("absent effort must stay absent (model default): %v", reasoning)
	}

	m = mustTranslate(t, `{"model":"m","reasoning_effort":"none","messages":[{"role":"user","content":"hi"}]}`, "m")
	reasoning, _ = m["reasoning"].(map[string]any)
	if reasoning["effort"] != "none" {
		t.Errorf("reasoning = %v, want effort=none", m["reasoning"])
	}
	if _, hasSummary := reasoning["summary"]; hasSummary {
		t.Errorf("effort=none must not request a summary: %v", reasoning)
	}
}

// Legacy max_tokens maps when max_completion_tokens is absent; the modern
// field wins when both are present.
func TestTranslateChat_MaxTokens(t *testing.T) {
	m := mustTranslate(t, `{"model":"m","max_tokens":100,"messages":[]}`, "m")
	if m["max_output_tokens"] != float64(100) {
		t.Errorf("max_output_tokens = %v, want 100", m["max_output_tokens"])
	}
	m = mustTranslate(t, `{"model":"m","max_tokens":100,"max_completion_tokens":200,"messages":[]}`, "m")
	if m["max_output_tokens"] != float64(200) {
		t.Errorf("max_output_tokens = %v, want 200 (modern field wins)", m["max_output_tokens"])
	}
}

// Unsupported/unknown chat params must not leak into the Responses body.
func TestTranslateChat_DropsUnknownParams(t *testing.T) {
	body := `{"model":"m","messages":[],"stream_options":{"include_usage":true},"frequency_penalty":0.5,"stop":["x"],"n":2}`
	m := mustTranslate(t, body, "m")
	for _, banned := range []string{"stream_options", "frequency_penalty", "stop", "n", "messages", "reasoning_effort", "max_tokens"} {
		if _, ok := m[banned]; ok {
			t.Errorf("param %q leaked into Responses body", banned)
		}
	}
}

// tool_choice translation: strings pass through, the named-function object
// flattens.
func TestTranslateChat_ToolChoice(t *testing.T) {
	m := mustTranslate(t, `{"model":"m","messages":[],"tool_choice":"required"}`, "m")
	if m["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v", m["tool_choice"])
	}
	m = mustTranslate(t, `{"model":"m","messages":[],"tool_choice":{"type":"function","function":{"name":"f"}}}`, "m")
	tc, _ := m["tool_choice"].(map[string]any)
	if tc["type"] != "function" || tc["name"] != "f" {
		t.Errorf("tool_choice = %v, want flattened named function", m["tool_choice"])
	}
	if _, nested := tc["function"]; nested {
		t.Errorf("tool_choice still nested: %v", tc)
	}
}

// response_format maps to text.format: json_object verbatim, json_schema
// flattened.
func TestTranslateChat_ResponseFormat(t *testing.T) {
	m := mustTranslate(t, `{"model":"m","messages":[],"response_format":{"type":"json_object"}}`, "m")
	text, _ := m["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_object" {
		t.Errorf("text.format = %v", m["text"])
	}

	m = mustTranslate(t, `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"},"strict":true}}}`, "m")
	text, _ = m["text"].(map[string]any)
	format, _ = text["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "answer" || format["strict"] != true {
		t.Errorf("text.format = %v, want flattened json_schema", format)
	}
	if _, ok := format["schema"]; !ok {
		t.Errorf("schema dropped: %v", format)
	}

	m = mustTranslate(t, `{"model":"m","messages":[],"response_format":{"type":"text"}}`, "m")
	if _, ok := m["text"]; ok {
		t.Errorf("type:text should omit the text config, got %v", m["text"])
	}
}

// An assistant tool call with empty arguments gets the "{}" placeholder, and
// a developer role folds into instructions alongside system.
func TestTranslateChat_EdgeCases(t *testing.T) {
	body := `{"model":"m","messages":[
		{"role":"developer","content":"dev rules"},
		{"role":"system","content":"sys rules"},
		{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":""}}]}
	]}`
	m := mustTranslate(t, body, "m")
	if m["instructions"] != "dev rules\n\nsys rules" {
		t.Errorf("instructions = %q", m["instructions"])
	}
	items := inputItems(t, m)
	if len(items) != 1 || items[0]["arguments"] != "{}" {
		t.Errorf("empty arguments not defaulted: %v", items)
	}
}

func TestTranslateChat_InvalidBody(t *testing.T) {
	if _, err := TranslateChatToResponses([]byte("not json"), "m"); err == nil {
		t.Error("want error for invalid body")
	}
}
