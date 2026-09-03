package paramrewrite

import (
	"encoding/json"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// SchemaFallbackKey is the pseudo-param the learned caches carry for a
// provider that has refused response_format type json_schema. It is not a
// request field, so the strip phases delete nothing for it; BuildUpstreamBody
// reads it and rewrites the request into JSON mode instead.
const SchemaFallbackKey = "response_format.json_schema"

// jsonModeOnlyProviders are the provider types whose API documents JSON mode
// (response_format {"type":"json_object"}) and nothing schema-shaped, so a
// json_schema request is rewritten before the first attempt rather than after
// a 400 per model per restart. DeepSeek answers json_schema with
// "This response_format type is unavailable now" on every current model and
// its JSON Output guide lists json_object alone.
var jsonModeOnlyProviders = map[string]bool{
	"deepseek": true,
}

// schemaRefusalNames are the tokens a 400 refusing the json_schema shape names.
// A message about response_format that is not about the schema (DeepSeek's
// "Prompt must contain the word 'json'" for JSON mode) teaches the same key,
// which is harmless: the rewrite only touches a request that carries
// json_schema, and that request was already rejected.
var schemaRefusalNames = []string{"response_format", "json_schema"}

// refusesJSONSchema reports whether a 400 message is about the response
// format, which for a request that sent json_schema means the provider does
// not serve that shape.
func refusesJSONSchema(msg string) bool {
	for _, name := range schemaRefusalNames {
		if strings.Contains(msg, name) {
			return true
		}
	}
	return false
}

// downgradeJSONSchema rewrites a response_format of type json_schema into JSON
// mode with the schema folded into the prompt, the shape a JSON-mode-only
// provider can serve: the model is told to answer with one JSON object
// conforming to the schema, which every such provider requires the prompt to
// ask for in some form anyway. The instruction joins an existing leading
// system message (a second system turn is not accepted everywhere) and is
// prepended as one when there is none. The provider does not validate the
// output against the schema, so the caller's strict flag is a request the
// answer may not honour; it is logged, not signalled, since the response
// shape has no field for it. Reports whether the request was rewritten.
func downgradeJSONSchema(raw map[string]any, modelID string) bool {
	rf, ok := raw["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_schema" {
		return false
	}
	schema := rf["json_schema"]
	if wrapped, ok := schema.(map[string]any); ok {
		if inner, ok := wrapped["schema"]; ok {
			schema = inner
		}
	}
	instruction := "Respond with a single JSON object and nothing else."
	if schemaJSON, err := json.Marshal(schema); err == nil && schema != nil {
		instruction += " It must conform to this JSON Schema:\n" + string(schemaJSON)
	}
	raw["response_format"] = map[string]any{"type": "json_object"}
	messages, _ := raw["messages"].([]any)
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]any); ok && first["role"] == "system" {
			if content, ok := first["content"].(string); ok {
				first["content"] = content + "\n\n" + instruction
				debuglog.Debug("paramrewrite: json_schema rewritten to JSON mode with the schema in the prompt", "model", modelID)
				return true
			}
		}
	}
	raw["messages"] = append([]any{map[string]any{"role": "system", "content": instruction}}, messages...)
	debuglog.Debug("paramrewrite: json_schema rewritten to JSON mode with the schema in the prompt", "model", modelID)
	return true
}
