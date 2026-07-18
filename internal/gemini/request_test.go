package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeGemini unmarshals a translated generateContent body for assertions.
func decodeGemini(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("translated body is not valid JSON: %v\n%s", err, body)
	}
	return m
}

func TestTranslateRequest_SystemAndText(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "system", "content": "Be terse."},
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
			{"role": "user", "content": "Bye"}
		],
		"max_tokens": 128,
		"temperature": 0.5,
		"top_p": 0.9,
		"stop": "END"
	}`)

	out, model, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	if model != "gemini-2.5-flash" {
		t.Errorf("model = %q, want gemini-2.5-flash", model)
	}
	if stream {
		t.Error("stream = true, want false")
	}

	m := decodeGemini(t, out)
	sys := m["systemInstruction"].(map[string]any)
	parts := sys["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "Be terse." {
		t.Errorf("systemInstruction = %v", sys)
	}

	contents := m["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(contents))
	}
	first := contents[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("contents[0] role = %v, want user", first["role"])
	}
	second := contents[1].(map[string]any)
	if second["role"] != "model" {
		t.Errorf("contents[1] role = %v, want model (assistant must map)", second["role"])
	}
	if second["parts"].([]any)[0].(map[string]any)["text"] != "Hi" {
		t.Errorf("contents[1] parts = %v", second["parts"])
	}

	gc := m["generationConfig"].(map[string]any)
	if gc["maxOutputTokens"] != float64(128) {
		t.Errorf("maxOutputTokens = %v, want 128", gc["maxOutputTokens"])
	}
	if gc["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", gc["temperature"])
	}
	if gc["topP"] != 0.9 {
		t.Errorf("topP = %v, want 0.9", gc["topP"])
	}
	stops := gc["stopSequences"].([]any)
	if len(stops) != 1 || stops[0] != "END" {
		t.Errorf("stopSequences = %v, want [END]", stops)
	}

	// The OpenAI model/messages keys must not leak into the Gemini body.
	for _, k := range []string{"model", "messages", "max_tokens"} {
		if _, ok := m[k]; ok {
			t.Errorf("OpenAI key %q leaked into gemini body", k)
		}
	}
}

func TestTranslateRequest_StreamFlagAndMaxCompletionTokens(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-pro",
		"messages": [{"role": "user", "content": "hi"}],
		"stream": true,
		"max_tokens": 10,
		"max_completion_tokens": 42,
		"stop": ["a", "b"]
	}`)

	out, model, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	if model != "gemini-2.5-pro" || !stream {
		t.Errorf("model/stream = %q/%v, want gemini-2.5-pro/true", model, stream)
	}
	gc := decodeGemini(t, out)["generationConfig"].(map[string]any)
	// max_completion_tokens is the modern field and wins over max_tokens.
	if gc["maxOutputTokens"] != float64(42) {
		t.Errorf("maxOutputTokens = %v, want 42", gc["maxOutputTokens"])
	}
	stops := gc["stopSequences"].([]any)
	if len(stops) != 2 || stops[0] != "a" || stops[1] != "b" {
		t.Errorf("stopSequences = %v, want [a b]", stops)
	}
}

func TestTranslateRequest_ImageParts(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "What is this?"},
			{"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}},
			{"type": "image_url", "image_url": {"url": "https://example.com/cat.png"}}
		]}]
	}`)

	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	parts := decodeGemini(t, out)["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(parts) != 3 {
		t.Fatalf("parts len = %d, want 3", len(parts))
	}
	if parts[0].(map[string]any)["text"] != "What is this?" {
		t.Errorf("parts[0] = %v", parts[0])
	}
	inline := parts[1].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/png" || inline["data"] != "AAAA" {
		t.Errorf("inlineData = %v", inline)
	}
	file := parts[2].(map[string]any)["fileData"].(map[string]any)
	if file["fileUri"] != "https://example.com/cat.png" {
		t.Errorf("fileData = %v", file)
	}
}

func TestTranslateRequest_ToolsAndToolChoice(t *testing.T) {
	base := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [{"type": "function", "function": {
			"name": "get_weather",
			"description": "Get weather",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
		}}],
		"tool_choice": %s
	}`

	tests := []struct {
		choice  string
		mode    string
		allowed string
	}{
		{`"auto"`, "AUTO", ""},
		{`"none"`, "NONE", ""},
		{`"required"`, "ANY", ""},
		{`{"type": "function", "function": {"name": "get_weather"}}`, "ANY", "get_weather"},
	}
	for _, tc := range tests {
		out, _, _, err := TranslateRequest([]byte(strings.Replace(base, "%s", tc.choice, 1)))
		if err != nil {
			t.Fatalf("TranslateRequest(%s) failed: %v", tc.choice, err)
		}
		m := decodeGemini(t, out)

		tools := m["tools"].([]any)
		decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
		fd := decls[0].(map[string]any)
		if fd["name"] != "get_weather" || fd["description"] != "Get weather" {
			t.Errorf("functionDeclaration = %v", fd)
		}
		if _, ok := fd["parameters"].(map[string]any)["properties"]; !ok {
			t.Errorf("parameters not carried: %v", fd["parameters"])
		}

		fcc := m["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
		if fcc["mode"] != tc.mode {
			t.Errorf("tool_choice %s: mode = %v, want %s", tc.choice, fcc["mode"], tc.mode)
		}
		if tc.allowed == "" {
			if _, ok := fcc["allowedFunctionNames"]; ok {
				t.Errorf("tool_choice %s: unexpected allowedFunctionNames", tc.choice)
			}
		} else {
			names := fcc["allowedFunctionNames"].([]any)
			if len(names) != 1 || names[0] != tc.allowed {
				t.Errorf("tool_choice %s: allowedFunctionNames = %v", tc.choice, names)
			}
		}
	}
}

func TestTranslateRequest_ToolCallsAndToolResults(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": "weather in Oslo?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Oslo\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "{\"temp\": -3}"},
			{"role": "tool", "tool_call_id": "call_unknown", "content": "plain text result"}
		]
	}`)

	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	contents := decodeGemini(t, out)["contents"].([]any)
	if len(contents) != 4 {
		t.Fatalf("contents len = %d, want 4", len(contents))
	}

	// Assistant tool-call turn -> model functionCall part with args as an object.
	fc := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "get_weather" {
		t.Errorf("functionCall name = %v", fc["name"])
	}
	if fc["args"].(map[string]any)["city"] != "Oslo" {
		t.Errorf("functionCall args = %v", fc["args"])
	}

	// Tool result -> user functionResponse; name resolved via the tool_call_id.
	toolMsg := contents[2].(map[string]any)
	if toolMsg["role"] != "user" {
		t.Errorf("tool message role = %v, want user", toolMsg["role"])
	}
	fr := toolMsg["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "get_weather" {
		t.Errorf("functionResponse name = %v, want get_weather", fr["name"])
	}
	// JSON-object tool output passes through as the response object.
	if fr["response"].(map[string]any)["temp"] != float64(-3) {
		t.Errorf("functionResponse response = %v", fr["response"])
	}

	// Unknown tool_call_id: name falls back to the id; plain text is wrapped.
	fr2 := contents[3].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr2["name"] != "call_unknown" {
		t.Errorf("functionResponse name = %v, want call_unknown", fr2["name"])
	}
	if fr2["response"].(map[string]any)["result"] != "plain text result" {
		t.Errorf("functionResponse response = %v", fr2["response"])
	}
}

func TestTranslateRequest_ReasoningEffort(t *testing.T) {
	tests := []struct {
		effort string
		budget float64
	}{
		{"none", 0},
		{"low", 1024},
		{"medium", 8192},
		{"high", 24576},
	}
	for _, tc := range tests {
		body := []byte(`{"model": "gemini-2.5-flash", "messages": [{"role": "user", "content": "hi"}], "reasoning_effort": "` + tc.effort + `"}`)
		out, _, _, err := TranslateRequest(body)
		if err != nil {
			t.Fatalf("TranslateRequest(%s) failed: %v", tc.effort, err)
		}
		gc := decodeGemini(t, out)["generationConfig"].(map[string]any)
		think := gc["thinkingConfig"].(map[string]any)
		if think["thinkingBudget"] != tc.budget {
			t.Errorf("effort %s: thinkingBudget = %v, want %v", tc.effort, think["thinkingBudget"], tc.budget)
		}
	}

	// No reasoning_effort -> no thinkingConfig (model default).
	out, _, _, err := TranslateRequest([]byte(`{"model": "m", "messages": [{"role": "user", "content": "hi"}]}`))
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	if gc, ok := decodeGemini(t, out)["generationConfig"].(map[string]any); ok {
		if _, ok := gc["thinkingConfig"]; ok {
			t.Error("thinkingConfig present without reasoning_effort")
		}
	}
}

func TestTranslateRequest_JSONResponseFormat(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"response_format": {"type": "json_object"}
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	gc := decodeGemini(t, out)["generationConfig"].(map[string]any)
	if gc["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v, want application/json", gc["responseMimeType"])
	}
	if _, ok := gc["responseJsonSchema"]; ok {
		t.Error("responseJsonSchema present for schemaless json_object")
	}
}

func TestTranslateRequest_JSONSchemaResponseFormat(t *testing.T) {
	// Structured output must forward the schema, not downgrade to generic JSON
	// mode. Vertex's responseJsonSchema accepts standard JSON Schema verbatim
	// (live-verified 2026-07-18, incl. additionalProperties).
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"response_format": {"type": "json_schema", "json_schema": {
			"name": "weather",
			"strict": true,
			"schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"], "additionalProperties": false}
		}}
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	gc := decodeGemini(t, out)["generationConfig"].(map[string]any)
	if gc["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v, want application/json", gc["responseMimeType"])
	}
	schema := gc["responseJsonSchema"].(map[string]any)
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Errorf("responseJsonSchema = %v, want schema forwarded verbatim", schema)
	}
	if _, ok := schema["properties"].(map[string]any)["city"]; !ok {
		t.Errorf("responseJsonSchema properties = %v", schema["properties"])
	}
}

func TestTranslateRequest_SystemAsParts(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "system", "content": [{"type": "text", "text": "Be "}, {"type": "text", "text": "terse."}]},
			{"role": "user", "content": "hi"}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	sys := decodeGemini(t, out)["systemInstruction"].(map[string]any)
	if sys["parts"].([]any)[0].(map[string]any)["text"] != "Be terse." {
		t.Errorf("systemInstruction = %v, want flattened part text", sys)
	}
}

func TestTranslateRequest_EdgeCases(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "look"},
				{"type": "image_url", "image_url": {"url": "data:image/png,not-base64"}}
			]},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "f", "arguments": "not json"}}
			]}
		],
		"stop": "",
		"tool_choice": "weird"
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	m := decodeGemini(t, out)

	// Malformed data: URI (no ;base64, segment) is dropped, text kept.
	parts := m["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(parts) != 1 {
		t.Errorf("parts = %v, want malformed image dropped", parts)
	}
	// Invalid tool_call arguments fall back to {}.
	fc := m["contents"].([]any)[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if len(fc["args"].(map[string]any)) != 0 {
		t.Errorf("args = %v, want {}", fc["args"])
	}
	// Empty stop string and unknown tool_choice are both omitted.
	if gc, ok := m["generationConfig"]; ok {
		if _, ok := gc.(map[string]any)["stopSequences"]; ok {
			t.Error("stopSequences present for empty stop")
		}
	}
	if _, ok := m["toolConfig"]; ok {
		t.Error("toolConfig present for unknown tool_choice")
	}
}

func TestTranslateRequest_InvalidMessageContent(t *testing.T) {
	body := []byte(`{"model": "m", "messages": [{"role": "user", "content": {"bogus": true}}]}`)
	if _, _, _, err := TranslateRequest(body); err == nil {
		t.Error("expected error for object-shaped message content")
	}
}

func TestTranslateRequest_Errors(t *testing.T) {
	if _, _, _, err := TranslateRequest([]byte(`{not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if _, _, _, err := TranslateRequest([]byte(`{"messages": [{"role": "user", "content": "hi"}]}`)); err == nil {
		t.Error("expected error for missing model")
	}
}
