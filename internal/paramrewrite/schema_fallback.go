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
// its JSON Output guide lists json_object alone; Z.AI's structured output
// guide documents json_object alone, and its models answer json_schema with
// a fenced block of their own shape.
var jsonModeOnlyProviders = map[string]bool{
	"deepseek":   true,
	"zai-coding": true,
}

// schemaIgnoredByModel reports a model family that takes response_format
// json_schema without error and answers in a shape of its own, wherever it
// is hosted: GLM does so on Z.AI, Ollama Cloud and OpenCode Go alike, with
// the JSON fenced as markdown. For such a model the schema is folded into
// the prompt while json_schema stays on the request, so a host that does
// enforce it still can, and one that does not gets the shape from the
// prompt; observed to answer bare, schema-shaped JSON on all three.
func schemaIgnoredByModel(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "glm")
}

// schemaRefusalNames are the tokens a 400 about the response format names,
// and schemaRefusalPhrases the words that make it a refusal of the shape
// rather than of this request's schema: OpenAI's "Invalid schema for
// response_format 'x': 'additionalProperties' is required" is a complaint
// about one caller's schema, and learning the fallback from it would strip
// every later caller on that model of the schema enforcement the provider
// has. Only a shape the provider says it does not serve is learned, and
// unconditionally: schemaRefusalExcludes name a path inside one caller's
// schema (a keyword it used that the provider lacks) or a condition ("not
// supported when tools are provided") that another request may not meet.
var (
	schemaRefusalNames    = []string{"response_format", "json_schema"}
	schemaRefusalPhrases  = []string{"unavailable", "not available", "not supported", "unsupported", "does not support", "not implemented"}
	schemaRefusalExcludes = []string{"invalid schema", "json_schema.schema", "schema.properties", " when ", " with tools"}
)

// refusesJSONSchema reports whether a 400 message says the provider does not
// serve the response format shape it was sent.
func refusesJSONSchema(msg string) bool {
	lower := strings.ToLower(msg)
	for _, exclude := range schemaRefusalExcludes {
		if strings.Contains(lower, exclude) {
			return false
		}
	}
	named := false
	for _, name := range schemaRefusalNames {
		if strings.Contains(lower, name) {
			named = true
			break
		}
	}
	if !named {
		return false
	}
	for _, phrase := range schemaRefusalPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// RequestsJSONSchema reports whether a chat-completions body asks for
// response_format type json_schema.
func RequestsJSONSchema(body []byte) bool {
	var req struct {
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
	}
	return json.Unmarshal(body, &req) == nil && req.ResponseFormat.Type == "json_schema"
}

// DropSchemaFallbackUnlessRequested removes the schema fallback from a learned
// set when the request that drew the 400 did not send json_schema: the
// message then complained about something else in the response format (JSON
// mode wanting the word json in the prompt), which the fallback cannot fix and
// a retry would only repeat. The parser reads the 400 alone, so the request
// side of the judgement lives here, at the callers that hold both.
func DropSchemaFallbackUnlessRequested(rejected map[string]bool, requestBody []byte) map[string]bool {
	if !rejected[SchemaFallbackKey] || RequestsJSONSchema(requestBody) {
		return rejected
	}
	delete(rejected, SchemaFallbackKey)
	if len(rejected) == 0 {
		return nil
	}
	return rejected
}

// schemaPromptMax bounds the schema text folded into the prompt. Past it the
// plain JSON-mode instruction goes alone: the schema is billed as input on
// every request, and one large enough to crowd the context would turn a 400
// this heals into a context-length 400 nothing heals.
const schemaPromptMax = 8 << 10

// foldJSONSchema folds a response_format of type json_schema into the prompt:
// the model is told to answer with one JSON object conforming to the schema,
// which every JSON-mode provider requires the prompt to ask for in some form
// anyway. With keepSchema the request keeps json_schema for a host that
// enforces it; without, it becomes JSON mode (json_object), the shape a
// JSON-mode-only provider can serve. The instruction joins a leading system or
// developer turn (appended to string content, a text part on content parts,
// the content itself when there is none), since a second system turn is not
// accepted everywhere, and is prepended as a system turn when there is no
// such turn or its content has a shape the join cannot keep.
// A json_schema with no schema object, or one past schemaPromptMax, gets the
// plain JSON-mode instruction. A JSON-mode provider does not validate the
// output against the schema, so the caller's strict flag is a request the
// answer may not honour; the debug line carries it, since the response shape
// has no field for it.
func foldJSONSchema(raw map[string]any, modelID string, keepSchema bool) {
	rf, ok := raw["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_schema" {
		return
	}
	instruction := "Respond with a single JSON object and nothing else."
	var strict any
	if wrapped, ok := rf["json_schema"].(map[string]any); ok {
		strict = wrapped["strict"]
		if schema, ok := wrapped["schema"].(map[string]any); ok {
			if schemaJSON, err := json.Marshal(schema); err == nil && len(schemaJSON) <= schemaPromptMax {
				instruction += " It must conform to this JSON Schema:\n" + string(schemaJSON)
			}
		}
	}
	if !keepSchema {
		raw["response_format"] = map[string]any{"type": "json_object"}
	}
	debuglog.Debug("paramrewrite: json_schema folded into the prompt", "model", modelID, "strict", strict, "json_schema_kept", keepSchema)
	messages, _ := raw["messages"].([]any)
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]any); ok && (first["role"] == "system" || first["role"] == "developer") {
			switch content := first["content"].(type) {
			case string:
				first["content"] = content + "\n\n" + instruction
			case []any:
				first["content"] = append(content, map[string]any{"type": "text", "text": instruction})
			case nil:
				first["content"] = instruction
			default:
				raw["messages"] = append([]any{map[string]any{"role": "system", "content": instruction}}, messages...)
			}
			return
		}
	}
	raw["messages"] = append([]any{map[string]any{"role": "system", "content": instruction}}, messages...)
}
