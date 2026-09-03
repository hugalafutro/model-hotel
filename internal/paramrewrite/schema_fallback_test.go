package paramrewrite

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

const deepSeekSchemaRefusal = `{"error":{"message":"This response_format type is unavailable now","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`

func schemaRequest(system string) []byte {
	msgs := `[{"role":"user","content":"Facts about Tokyo."}]`
	if system != "" {
		msgs = `[{"role":"system","content":"` + system + `"},{"role":"user","content":"Facts about Tokyo."}]`
	}
	return []byte(`{"model":"m","messages":` + msgs + `,"response_format":{"type":"json_schema","json_schema":{"name":"city","strict":true,"schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}}`)
}

func decodeJSONBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return raw
}

// A 400 about the response format is learned as the schema fallback key, not
// as a strip of response_format.
func TestParseProviderParamError_SchemaRefusalLearnsTheFallback(t *testing.T) {
	rejected := ParseProviderParamError([]byte(deepSeekSchemaRefusal))
	if !rejected[SchemaFallbackKey] {
		t.Fatalf("rejected = %v, want %s", rejected, SchemaFallbackKey)
	}
	if rejected["response_format"] {
		t.Error("response_format itself must not be stripped")
	}
	if ParseProviderParamError([]byte(`{"error":{"message":"Unsupported parameter: 'temperature'"}}`))[SchemaFallbackKey] {
		t.Error("a 400 about another param must not learn the fallback")
	}
}

// A JSON-mode-only provider type gets json_schema rewritten on the first
// attempt: JSON mode, the schema in a new leading system message.
func TestBuildUpstreamBody_JSONModeOnlyProviderDowngradesSchema(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	rf := raw["response_format"].(map[string]any)
	if rf["type"] != "json_object" || rf["json_schema"] != nil {
		t.Fatalf("response_format = %v, want plain json_object", rf)
	}
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want the instruction prepended", len(msgs))
	}
	first := msgs[0].(map[string]any)
	content, _ := first["content"].(string)
	if first["role"] != "system" || !strings.Contains(content, `"required":["city"]`) || !strings.Contains(strings.ToLower(content), "json") {
		t.Errorf("leading message = %v, want a system turn carrying the schema and the word json", first)
	}
	if msgs[1].(map[string]any)["role"] != "user" {
		t.Error("the caller's messages must follow the instruction")
	}
}

// The instruction joins an existing leading system message rather than adding
// a second system turn.
func TestBuildUpstreamBody_SchemaInstructionJoinsTheSystemMessage(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest("You are terse."), "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want no extra turn", len(msgs))
	}
	content := msgs[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(content, "You are terse.") || !strings.Contains(content, "JSON Schema") {
		t.Errorf("system content = %q, want the caller's text followed by the instruction", content)
	}
}

// Other provider types keep json_schema until a 400 teaches the fallback,
// through the learned cache or the retry's extra strip, and a request in JSON
// mode already is left alone either way.
func TestBuildUpstreamBody_SchemaFallbackOnlyWhenLearned(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_schema" {
		t.Fatal("an unlearned provider must keep json_schema")
	}
	raw = decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, map[string]bool{SchemaFallbackKey: true}, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_object" {
		t.Error("the retry's extra strip must apply the fallback")
	}
	MergeLearnedParamCache(&dep, LearnedCacheKey("p", "m"), map[string]bool{SchemaFallbackKey: true})
	raw = decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_object" {
		t.Error("the learned cache must apply the fallback on later requests")
	}
	if _, present := raw[SchemaFallbackKey]; present {
		t.Error("the pseudo-param must never reach the upstream body")
	}
	plain := []byte(`{"model":"m","messages":[{"role":"user","content":"json please"}],"response_format":{"type":"json_object"}}`)
	raw = decodeJSONBody(t, BuildUpstreamBody(plain, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	if len(raw["messages"].([]any)) != 1 {
		t.Error("a JSON-mode request must not gain an instruction")
	}
}
