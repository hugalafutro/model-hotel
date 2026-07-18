// Package gemini translates between the OpenAI chat-completions wire shape and
// Google's Gemini generateContent shape. It is MH's first *egress* dialect
// adapter (the mirror image of internal/anthropic, which translates on
// ingress): Vertex AI express-mode API keys only work on the native
// publisher routes, so requests leaving MH for a vertex-express provider are
// rewritten here and the responses translated back.
package gemini

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Stop                json.RawMessage `json:"stop"` // string OR []string
	Tools               []oaiTool       `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	ResponseFormat      *oaiRespFormat  `json:"response_format"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []oaiContentPart OR null
	ToolCalls  []oaiToolCall   `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type oaiContentPart struct {
	Type     string `json:"type"` // "text" | "image_url"
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
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
	Type string `json:"type"` // "text" | "json_object" | "json_schema"
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

type genFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type genToolConfig struct {
	FunctionCallingConfig genFunctionCallingConfig `json:"functionCallingConfig"`
}

type genFunctionCallingConfig struct {
	Mode                 string   `json:"mode"` // AUTO | ANY | NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type genConfig struct {
	MaxOutputTokens  int                `json:"maxOutputTokens,omitempty"`
	Temperature      *float64           `json:"temperature,omitempty"`
	TopP             *float64           `json:"topP,omitempty"`
	StopSequences    []string           `json:"stopSequences,omitempty"`
	ResponseMimeType string             `json:"responseMimeType,omitempty"`
	ThinkingConfig   *genThinkingConfig `json:"thinkingConfig,omitempty"`
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
// string (verbatim — the caller builds the :generateContent /
// :streamGenerateContent URL from it), and the stream flag.
func TranslateRequest(body []byte) (geminiBody []byte, model string, stream bool, err error) {
	var req oaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", false, fmt.Errorf("gemini: invalid request body: %w", err)
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
			out.Contents = append(out.Contents, genContent{
				Role:  "user",
				Parts: []genPart{{FunctionResponse: &genFunctionResp{Name: name, Response: toolResponseValue(m.Content)}}},
			})
		case "assistant":
			parts, err := translateParts(m.Content)
			if err != nil {
				return nil, "", false, err
			}
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Function.Name
				args := json.RawMessage(tc.Function.Arguments)
				if len(args) == 0 || !json.Valid(args) {
					args = json.RawMessage("{}")
				}
				parts = append(parts, genPart{FunctionCall: &genFunctionCall{Name: tc.Function.Name, Args: args}})
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
	}
	if len(systemParts) > 0 {
		out.SystemInstruction = &genContent{Parts: systemParts}
	}

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, genTool{FunctionDeclarations: []genFunctionDecl{{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		}}})
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
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: decodeStop(req.Stop),
	}
	// max_completion_tokens is the modern OpenAI field and wins over the
	// deprecated max_tokens when both are present.
	gc.MaxOutputTokens = req.MaxCompletionTokens
	if gc.MaxOutputTokens == 0 {
		gc.MaxOutputTokens = req.MaxTokens
	}
	if req.ResponseFormat != nil && (req.ResponseFormat.Type == "json_object" || req.ResponseFormat.Type == "json_schema") {
		gc.ResponseMimeType = "application/json"
	}
	if budget, ok := reasoningBudgets[strings.ToLower(req.ReasoningEffort)]; ok && req.ReasoningEffort != "" {
		gc.ThinkingConfig = &genThinkingConfig{ThinkingBudget: budget}
	}

	if gc.MaxOutputTokens == 0 && gc.Temperature == nil && gc.TopP == nil &&
		len(gc.StopSequences) == 0 && gc.ResponseMimeType == "" && gc.ThinkingConfig == nil {
		return nil
	}
	return &gc
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
		return nil, fmt.Errorf("gemini: invalid message content: %w", err)
	}
	var parts []genPart
	for _, p := range oaiParts {
		switch p.Type {
		case "text":
			parts = append(parts, genPart{Text: p.Text})
		case "image_url":
			if p.ImageURL == nil || p.ImageURL.URL == "" {
				continue
			}
			if part, ok := imagePart(p.ImageURL.URL); ok {
				parts = append(parts, part)
			}
		}
	}
	return parts, nil
}

// imagePart maps an OpenAI image_url value to inlineData (data: URIs) or
// fileData (plain URLs).
func imagePart(u string) (genPart, bool) {
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

// decodeStop accepts OpenAI's string-or-array stop field.
func decodeStop(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	return nil
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
