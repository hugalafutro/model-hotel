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
		// Tool params ride parametersJsonSchema (full JSON Schema, verbatim —
		// live-verified 2026-07-18), not the OpenAPI-subset parameters field.
		if _, ok := fd["parametersJsonSchema"].(map[string]any)["properties"]; !ok {
			t.Errorf("parametersJsonSchema not carried: %v", fd["parametersJsonSchema"])
		}
		if _, ok := fd["parameters"]; ok {
			t.Errorf("legacy parameters field present: %v", fd["parameters"])
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

func TestTranslateRequest_MultipleToolsShareOneEntry(t *testing.T) {
	// All function declarations must ride ONE tools entry: Gemini treats
	// multiple tools array entries as multiple tool *types* and 400s with
	// "Multiple tools are supported only when they are all search tools"
	// (live, 2026-07-18).
	body := []byte(`{
		"model": "m",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"type": "function", "function": {"name": "f1", "parameters": {"type": "object"}}},
			{"type": "function", "function": {"name": "f2", "parameters": {"type": "object"}}}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	tools := decodeGemini(t, out)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools entries = %d, want 1", len(tools))
	}
	decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
	if len(decls) != 2 {
		t.Fatalf("functionDeclarations = %d, want 2", len(decls))
	}
	if decls[0].(map[string]any)["name"] != "f1" || decls[1].(map[string]any)["name"] != "f2" {
		t.Errorf("declarations = %v", decls)
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
	// Consecutive tool results coalesce into ONE user content: Gemini 400s
	// ("number of function response parts must equal function call parts")
	// when parallel results arrive as separate contents (live, 2026-07-18).
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3 (tool results coalesced)", len(contents))
	}

	// Assistant tool-call turn -> model functionCall part with args as an object.
	fc := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "get_weather" {
		t.Errorf("functionCall name = %v", fc["name"])
	}
	if fc["args"].(map[string]any)["city"] != "Oslo" {
		t.Errorf("functionCall args = %v", fc["args"])
	}

	// Both tool results ride one user content, one functionResponse part each,
	// names resolved via tool_call_id (falling back to the id when unknown).
	toolMsg := contents[2].(map[string]any)
	if toolMsg["role"] != "user" {
		t.Errorf("tool message role = %v, want user", toolMsg["role"])
	}
	parts := toolMsg["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("tool content parts = %d, want 2", len(parts))
	}
	fr := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "get_weather" {
		t.Errorf("functionResponse name = %v, want get_weather", fr["name"])
	}
	// JSON-object tool output passes through as the response object.
	if fr["response"].(map[string]any)["temp"] != float64(-3) {
		t.Errorf("functionResponse response = %v", fr["response"])
	}
	fr2 := parts[1].(map[string]any)["functionResponse"].(map[string]any)
	if fr2["name"] != "call_unknown" {
		t.Errorf("functionResponse name = %v, want call_unknown", fr2["name"])
	}
	if fr2["response"].(map[string]any)["result"] != "plain text result" {
		t.Errorf("functionResponse response = %v", fr2["response"])
	}
}

func TestTranslateRequest_ToolResultsSplitByOtherTurns(t *testing.T) {
	// Only CONSECUTIVE tool messages coalesce; a tool result after an
	// intervening turn starts a fresh content.
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "a", "type": "function", "function": {"name": "f1", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "a", "content": "r1"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "b", "type": "function", "function": {"name": "f2", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "b", "content": "r2"}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	contents := decodeGemini(t, out)["contents"].([]any)
	if len(contents) != 5 {
		t.Fatalf("contents len = %d, want 5 (separate rounds stay separate)", len(contents))
	}
	for _, idx := range []int{2, 4} {
		parts := contents[idx].(map[string]any)["parts"].([]any)
		if len(parts) != 1 {
			t.Errorf("contents[%d] parts = %d, want 1", idx, len(parts))
		}
	}
}

func TestTranslateRequest_PenaltiesAndSeed(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"frequency_penalty": 0.5,
		"presence_penalty": -0.3,
		"seed": 42
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	gc := decodeGemini(t, out)["generationConfig"].(map[string]any)
	if gc["frequencyPenalty"] != 0.5 {
		t.Errorf("frequencyPenalty = %v, want 0.5", gc["frequencyPenalty"])
	}
	if gc["presencePenalty"] != -0.3 {
		t.Errorf("presencePenalty = %v, want -0.3", gc["presencePenalty"])
	}
	if gc["seed"] != float64(42) {
		t.Errorf("seed = %v, want 42", gc["seed"])
	}

	// Absent knobs stay absent (zero values must not be sent).
	out, _, _, err = TranslateRequest([]byte(`{"model": "m", "messages": [{"role": "user", "content": "hi"}], "max_tokens": 5}`))
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	gc = decodeGemini(t, out)["generationConfig"].(map[string]any)
	for _, k := range []string{"frequencyPenalty", "presencePenalty", "seed"} {
		if _, ok := gc[k]; ok {
			t.Errorf("%s present without being requested", k)
		}
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

func TestTranslateRequest_EmptyTextPartsSkipped(t *testing.T) {
	// An empty text part would marshal as {} inside parts (Text has omitempty),
	// which Gemini rejects. Empty parts are dropped; a message left with no
	// parts is dropped entirely.
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": ""}, {"type": "text", "text": "hi"}]},
			{"role": "assistant", "content": [{"type": "text", "text": ""}]},
			{"role": "user", "content": [{"type": "text", "text": "bye"}]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	contents := decodeGemini(t, out)["contents"].([]any)
	if len(contents) != 2 {
		t.Fatalf("contents = %v, want empty-only assistant turn dropped", contents)
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["text"] != "hi" {
		t.Errorf("parts = %v, want empty text part dropped", parts)
	}
}

func TestTranslateRequest_NoTranslatableMessages(t *testing.T) {
	// Gemini requires at least one contents entry (live: 400 "at least one
	// contents field is required" for both null and []). Fail fast locally
	// instead of marshaling a request the upstream is guaranteed to reject.
	for _, body := range []string{
		`{"model": "m", "messages": []}`,
		`{"model": "m", "messages": [{"role": "system", "content": "sys only"}]}`,
		`{"model": "m", "messages": [{"role": "user", "content": ""}]}`,
	} {
		if _, _, _, err := TranslateRequest([]byte(body)); err == nil {
			t.Errorf("expected error for %s", body)
		}
	}
}
