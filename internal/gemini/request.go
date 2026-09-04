// Package gemini translates between the OpenAI chat-completions wire shape and
// Google's Gemini generateContent shape. Vertex AI express-mode API keys only
// work on the native publisher routes, so requests leaving MH for a
// vertex-express provider are rewritten here and the responses translated back.
package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// --- Incoming OpenAI chat-completions request shape ---
//
// Only the fields the translation needs are decoded; unknown fields are
// dropped (Gemini would reject them anyway, and the proxy's param-rewrite
// machinery never sees this path).

type oaiRequest struct {
	Model               string          `json:"model"`
	Messages            []oaiMessage    `json:"messages"`
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Stream              bool            `json:"stream"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	FrequencyPenalty    *float64        `json:"frequency_penalty"`
	PresencePenalty     *float64        `json:"presence_penalty"`
	Seed                *int64          `json:"seed"`
	Stop                json.RawMessage `json:"stop"` // string OR []string
	Tools               []oaiTool       `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	ResponseFormat      *oaiRespFormat  `json:"response_format"`
	// Modalities is the OpenAI-compatible request for image output from a
	// chat model ("modalities": ["image", "text"]). Gemini's image models
	// generate an image only when the request names IMAGE among its response
	// modalities; without it the model still produces the image and the API
	// then fails the response ("Unhandled generated data mime type").
	Modalities []string `json:"modalities"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []oaiContentPart OR null
	ToolCalls  []oaiToolCall   `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type oaiContentPart struct {
	Type     string `json:"type"` // "text" | "image_url" | "input_audio" | "file"
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
	InputAudio *struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	} `json:"input_audio"`
	// File carries a document as a data: URI in file_data, the only field
	// read here: a file_id refers to OpenAI's Files store, which Gemini
	// cannot fetch, so a part without file_data is dropped like a malformed
	// image.
	File *struct {
		FileData string `json:"file_data"`
	} `json:"file"`
}

// audioFormats are the input_audio format words Gemini accepts as an
// audio/<format> inlineData mime type. OpenAI's chat spec names wav and mp3;
// the rest are Gemini's own list. Anything else (pcm16, webm, m4a) is dropped
// like a malformed image rather than sent on to a certain 400.
var audioFormats = map[string]bool{"wav": true, "mp3": true, "aiff": true, "aac": true, "ogg": true, "flac": true}

type oaiToolCall struct {
	ID string `json:"id"`
	// ExtraContent is where the thought signature travels on the OpenAI side:
	// extra_content.google.thought_signature. Raw, read leniently, so a
	// member of an unexpected shape does not fail the conversation on every
	// retry.
	ExtraContent json.RawMessage `json:"extra_content"`
	Function     struct {
		Name string `json:"name"`
		// util.ToolArguments, not a plain string: the spec says a JSON string
		// and several providers send the object instead. Rejecting the object
		// form 400s the conversation on every retry, since each one replays
		// the same transcript.
		Arguments util.ToolArguments `json:"arguments"`
	} `json:"function"`
}

type oaiTool struct {
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaiRespFormat struct {
	Type       string `json:"type"` // "text" | "json_object" | "json_schema"
	JSONSchema *struct {
		Schema json.RawMessage `json:"schema"`
	} `json:"json_schema"`
}

// --- Outgoing Gemini generateContent request shape ---

type genRequest struct {
	Contents          []genContent   `json:"contents"`
	SystemInstruction *genContent    `json:"systemInstruction,omitempty"`
	Tools             []genTool      `json:"tools,omitempty"`
	ToolConfig        *genToolConfig `json:"toolConfig,omitempty"`
	GenerationConfig  *genConfig     `json:"generationConfig,omitempty"`
}

type genContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []genPart `json:"parts"`
}

type genPart struct {
	Text             string           `json:"text,omitempty"`
	InlineData       *genBlob         `json:"inlineData,omitempty"`
	FileData         *genFileData     `json:"fileData,omitempty"`
	FunctionCall     *genFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *genFunctionResp `json:"functionResponse,omitempty"`
	// ThoughtSignature rides beside a functionCall part on the way back in:
	// Gemini 3 signs each call it makes and refuses the follow-up turn
	// without the signature ("Function call is missing a thought_signature
	// in functionCall parts").
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

type genBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type genFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type genFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type genFunctionResp struct {
	Name     string `json:"name"`
	Response any    `json:"response"`
}

type genTool struct {
	FunctionDeclarations []genFunctionDecl `json:"functionDeclarations"`
}

// genFunctionDecl carries tool parameters as parametersJsonSchema, which
// accepts standard JSON Schema verbatim (additionalProperties included),
// unlike the older `parameters` OpenAPI subset, so strict-mode client schemas
// survive untouched.
type genFunctionDecl struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type genToolConfig struct {
	FunctionCallingConfig genFunctionCallingConfig `json:"functionCallingConfig"`
}

type genFunctionCallingConfig struct {
	Mode                 string   `json:"mode"` // AUTO | ANY | NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type genConfig struct {
	MaxOutputTokens    int                `json:"maxOutputTokens,omitempty"`
	Temperature        *float64           `json:"temperature,omitempty"`
	TopP               *float64           `json:"topP,omitempty"`
	FrequencyPenalty   *float64           `json:"frequencyPenalty,omitempty"`
	PresencePenalty    *float64           `json:"presencePenalty,omitempty"`
	Seed               *int64             `json:"seed,omitempty"`
	StopSequences      []string           `json:"stopSequences,omitempty"`
	ResponseMimeType   string             `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage    `json:"responseJsonSchema,omitempty"`
	ThinkingConfig     *genThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseModalities []string           `json:"responseModalities,omitempty"`
}

type genThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

// reasoningBudgets maps OpenAI reasoning_effort to a Gemini thinking budget
// (tokens). "none" pins the budget to 0, which disables thinking on models
// that allow it; absent effort omits thinkingConfig so the model default wins.
var reasoningBudgets = map[string]int{
	"none":    0,
	"minimal": 0,
	"low":     1024,
	"medium":  8192,
	"high":    24576,
}

// TranslateRequest converts an OpenAI chat-completions request body into a
// Gemini generateContent request body. It returns the Gemini JSON, the model
// string (verbatim: the caller builds the :generateContent or
// :streamGenerateContent URL from it), and the stream flag.
func TranslateRequest(body []byte) (geminiBody []byte, model string, stream bool, err error) {
	var req oaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", false, fmt.Errorf("gemini: invalid request body: %s", jsonfault.Describe(err, len(body)))
	}
	if req.Model == "" {
		return nil, "", false, fmt.Errorf("gemini: model is required")
	}

	out := genRequest{}

	// Gemini has no tool_call IDs; functionResponse is keyed by function name.
	// Track the id->name mapping from assistant tool_calls so role:"tool"
	// messages can be resolved back to the function they answer.
	callNames := map[string]string{}

	var systemParts []genPart
	// True while the last emitted content is a coalescing tool-response
	// content; any other message kind breaks the run.
	lastToolContent := false
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if text := decodeTextContent(m.Content); text != "" {
				systemParts = append(systemParts, genPart{Text: text})
			}
		case "tool":
			name := callNames[m.ToolCallID]
			if name == "" {
				name = m.ToolCallID
			}
			part := genPart{FunctionResponse: &genFunctionResp{Name: name, Response: toolResponseValue(m.Content)}}
			// Consecutive tool results must share ONE user content: Gemini
			// requires the function response part count to equal the function
			// call part count of the preceding model turn and 400s when
			// parallel results arrive as separate contents.
			if lastToolContent {
				out.Contents[len(out.Contents)-1].Parts = append(out.Contents[len(out.Contents)-1].Parts, part)
			} else {
				out.Contents = append(out.Contents, genContent{Role: "user", Parts: []genPart{part}})
			}
			lastToolContent = true
			continue
		case "assistant":
			parts, err := translateParts(m.Content)
			if err != nil {
				return nil, "", false, err
			}
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Function.Name
				// Gemini types functionCall.args as a Struct, so a non-object
				// forwarded verbatim is a 400 from the provider.
				args := util.ToolArgumentsObject(tc.Function.Arguments)
				parts = append(parts, genPart{
					FunctionCall:     &genFunctionCall{Name: tc.Function.Name, Args: args},
					ThoughtSignature: egress.ThoughtSignatureIn(tc.ExtraContent),
				})
			}
			if len(parts) > 0 {
				out.Contents = append(out.Contents, genContent{Role: "model", Parts: parts})
			}
		default: // "user"
			parts, err := translateParts(m.Content)
			if err != nil {
				return nil, "", false, err
			}
			if len(parts) > 0 {
				out.Contents = append(out.Contents, genContent{Role: "user", Parts: parts})
			}
		}
		lastToolContent = false
	}
	if len(systemParts) > 0 {
		out.SystemInstruction = &genContent{Parts: systemParts}
	}
	// Gemini rejects null and empty contents alike ("at least one contents
	// field is required"); fail fast with a clearer error than the upstream 400.
	if len(out.Contents) == 0 {
		return nil, "", false, fmt.Errorf("gemini: at least one user or assistant message with content is required")
	}

	// All function declarations share ONE tools entry: Gemini reads multiple
	// tools array entries as multiple tool *types* (search, code execution,
	// functions) and rejects the mix.
	if len(req.Tools) > 0 {
		decls := make([]genFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, genFunctionDecl{
				Name:                 t.Function.Name,
				Description:          t.Function.Description,
				ParametersJSONSchema: t.Function.Parameters,
			})
		}
		out.Tools = []genTool{{FunctionDeclarations: decls}}
	}

	if tc, ok := translateToolChoice(req.ToolChoice); ok {
		out.ToolConfig = &genToolConfig{FunctionCallingConfig: tc}
	}

	out.GenerationConfig = buildGenerationConfig(&req)

	geminiBody, err = json.Marshal(out)
	if err != nil {
		return nil, "", false, fmt.Errorf("gemini: marshal generateContent request: %w", err)
	}
	return geminiBody, req.Model, req.Stream, nil
}

// buildGenerationConfig maps sampling/output knobs; returns nil when nothing
// is set so the field is omitted entirely.
func buildGenerationConfig(req *oaiRequest) *genConfig {
	gc := genConfig{
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
		Seed:             req.Seed,
		StopSequences:    egress.DecodeStop(req.Stop),
	}
	// max_completion_tokens is the modern OpenAI field and wins over the
	// deprecated max_tokens when both are present.
	gc.MaxOutputTokens = req.MaxCompletionTokens
	if gc.MaxOutputTokens == 0 {
		gc.MaxOutputTokens = req.MaxTokens
	}
	if req.ResponseFormat != nil && (req.ResponseFormat.Type == "json_object" || req.ResponseFormat.Type == "json_schema") {
		gc.ResponseMimeType = "application/json"
		// Structured output: forward the JSON Schema verbatim. Vertex's
		// responseJsonSchema takes standard JSON Schema (unlike the older
		// responseSchema OpenAPI subset), so no sanitizing is needed.
		if js := req.ResponseFormat.JSONSchema; js != nil && len(js.Schema) > 0 {
			gc.ResponseJSONSchema = js.Schema
		}
	}
	if budget, ok := reasoningBudgets[strings.ToLower(req.ReasoningEffort)]; ok && req.ReasoningEffort != "" {
		gc.ThinkingConfig = &genThinkingConfig{ThinkingBudget: budget}
	}
	if wantsImageOutput(req.Modalities) {
		// The request's list, translated: image plus text asks for both,
		// image alone asks for the picture only. A request without the field
		// sends nothing; an image model returns its image by default, which
		// is what the text-only model probes rely on.
		gc.ResponseModalities = responseModalities(req.Modalities)
	}

	if gc.MaxOutputTokens == 0 && gc.Temperature == nil && gc.TopP == nil &&
		gc.FrequencyPenalty == nil && gc.PresencePenalty == nil && gc.Seed == nil &&
		len(gc.StopSequences) == 0 && gc.ResponseMimeType == "" && gc.ThinkingConfig == nil &&
		len(gc.ResponseModalities) == 0 {
		return nil
	}
	return &gc
}

// RequestWantsImage reports whether a chat request names image among its
// output modalities; the proxy uses it to pick the native route for a
// provider whose OpenAI-compatibility layer cannot return one.
func RequestWantsImage(body []byte) bool {
	var req struct {
		Modalities []string `json:"modalities"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	return wantsImageOutput(req.Modalities)
}

// responseModalities maps an OpenAI modalities list that names image onto
// Gemini's response modalities, keeping text only when the client asked for
// it.
func responseModalities(modalities []string) []string {
	out := []string{"IMAGE"}
	for _, m := range modalities {
		if strings.EqualFold(m, "text") {
			return []string{"TEXT", "IMAGE"}
		}
	}
	return out
}

// wantsImageOutput reports a modalities list that names image output.
func wantsImageOutput(modalities []string) bool {
	for _, m := range modalities {
		if strings.EqualFold(m, "image") {
			return true
		}
	}
	return false
}

// translateParts converts an OpenAI message content field (string, part array,
// or null) into Gemini parts.
func translateParts(raw json.RawMessage) ([]genPart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return nil, nil
		}
		return []genPart{{Text: s}}, nil
	}

	var oaiParts []oaiContentPart
	if err := json.Unmarshal(raw, &oaiParts); err != nil {
		return nil, fmt.Errorf("gemini: invalid message content: %s", jsonfault.Describe(err, len(raw)))
	}
	var parts []genPart
	for _, p := range oaiParts {
		switch p.Type {
		case "text":
			// An empty text part marshals as {} (Text is omitempty), which
			// Gemini rejects.
			if p.Text == "" {
				continue
			}
			parts = append(parts, genPart{Text: p.Text})
		case "image_url":
			if p.ImageURL == nil || p.ImageURL.URL == "" {
				continue
			}
			if part, ok := mediaPart(p.ImageURL.URL); ok {
				parts = append(parts, part)
			}
		case "input_audio":
			if p.InputAudio == nil || p.InputAudio.Data == "" {
				continue
			}
			format := strings.ToLower(p.InputAudio.Format)
			if !audioFormats[format] {
				continue
			}
			parts = append(parts, genPart{InlineData: &genBlob{MimeType: "audio/" + format, Data: p.InputAudio.Data}})
		case "file":
			// file_data is base64 inline by definition, so only a data: URI
			// is honoured; a plain URL here is not a fetch Gemini should make
			// on the client's behalf.
			if p.File == nil || !strings.HasPrefix(p.File.FileData, "data:") {
				continue
			}
			if part, ok := mediaPart(p.File.FileData); ok {
				parts = append(parts, part)
			}
		}
	}
	return parts, nil
}

// mediaPart maps an OpenAI image_url or file_data value to inlineData (data:
// URIs) or fileData (plain URLs).
func mediaPart(u string) (genPart, bool) {
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mime, data, found := strings.Cut(rest, ";base64,")
		if !found || data == "" {
			return genPart{}, false
		}
		return genPart{InlineData: &genBlob{MimeType: mime, Data: data}}, true
	}
	return genPart{FileData: &genFileData{FileURI: u}}, true
}

// decodeTextContent flattens a content field to plain text (for system turns).
func decodeTextContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var oaiParts []oaiContentPart
	if json.Unmarshal(raw, &oaiParts) == nil {
		var sb strings.Builder
		for _, p := range oaiParts {
			if p.Type == "" || p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// toolResponseValue builds the functionResponse.response object from a tool
// message's content: a JSON object passes through, anything else is wrapped
// as {"result": <text>} because Gemini requires an object here.
func toolResponseValue(raw json.RawMessage) any {
	text := decodeTextContent(raw)
	var obj map[string]any
	if json.Unmarshal([]byte(text), &obj) == nil && obj != nil {
		return obj
	}
	return map[string]any{"result": text}
}

// translateToolChoice maps the OpenAI tool_choice union onto Gemini's
// functionCallingConfig.
func translateToolChoice(raw json.RawMessage) (genFunctionCallingConfig, bool) {
	if len(raw) == 0 {
		return genFunctionCallingConfig{}, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s {
		case "auto":
			return genFunctionCallingConfig{Mode: "AUTO"}, true
		case "none":
			return genFunctionCallingConfig{Mode: "NONE"}, true
		case "required":
			return genFunctionCallingConfig{Mode: "ANY"}, true
		}
		return genFunctionCallingConfig{}, false
	}
	var tc struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &tc) == nil && tc.Function.Name != "" {
		return genFunctionCallingConfig{Mode: "ANY", AllowedFunctionNames: []string{tc.Function.Name}}, true
	}
	return genFunctionCallingConfig{}, false
}
