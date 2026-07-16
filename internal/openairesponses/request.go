package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Incoming OpenAI chat-completions request shape ---
//
// Decoded via typed structs so unknown/unsupported knobs (stream_options,
// penalties, stop, n, ...) are dropped rather than forwarded to an endpoint
// that strict-validates them. The proxy pre-cleans the body through
// paramrewrite.BuildUpstreamBody first, so learned strips/renames still apply.

type chatRequest struct {
	Model               string           `json:"model"`
	Messages            []chatReqMessage `json:"messages"`
	Tools               []chatReqTool    `json:"tools"`
	ToolChoice          json.RawMessage  `json:"tool_choice"`
	ParallelToolCalls   *bool            `json:"parallel_tool_calls"`
	MaxTokens           int              `json:"max_tokens"`
	MaxCompletionTokens int              `json:"max_completion_tokens"`
	ReasoningEffort     string           `json:"reasoning_effort"`
	ResponseFormat      json.RawMessage  `json:"response_format"`
	Temperature         *float64         `json:"temperature"`
	TopP                *float64         `json:"top_p"`
	Stream              bool             `json:"stream"`
	Metadata            json.RawMessage  `json:"metadata"`
}

type chatReqMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []content part
	ToolCalls  []chatToolCall  `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type chatReqTool struct {
	Type     string          `json:"type"`
	Function chatReqToolFunc `json:"function"`
}

type chatReqToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict"`
}

// chatContentPart is one part of an array-form message content.
type chatContentPart struct {
	Type     string `json:"type"` // text | image_url
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

// TranslateChatToResponses converts an OpenAI chat-completions request body
// into a Responses API request body for the given resolved upstream model.
// System/developer messages become top-level instructions, the conversation
// becomes input items (message / function_call / function_call_output),
// tool definitions are flattened, and reasoning_effort maps to reasoning
// (with summary:"auto" so the model's reasoning summary can be surfaced back
// as reasoning_content). store is always false: the gateway is stateless and
// each turn re-sends the full transcript (plan §4.2).
func TranslateChatToResponses(chatBody []byte, resolvedModel string) ([]byte, error) {
	var req chatRequest
	if err := json.Unmarshal(chatBody, &req); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid chat request body: %w", err)
	}

	out := Request{
		Model:             resolvedModel,
		ParallelToolCalls: req.ParallelToolCalls,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		Metadata:          req.Metadata,
		Store:             false,
		Stream:            req.Stream,
	}
	if out.Model == "" {
		out.Model = req.Model
	}

	// max_completion_tokens is the modern name (and what the param-rename
	// self-heal produces); prefer it over legacy max_tokens.
	if req.MaxCompletionTokens > 0 {
		out.MaxOutputTokens = req.MaxCompletionTokens
	} else if req.MaxTokens > 0 {
		out.MaxOutputTokens = req.MaxTokens
	}

	instructions, input, err := translateChatMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	out.Instructions = instructions
	// input is required by the Responses API; an empty transcript still sends [].
	if input == nil {
		input = []any{}
	}
	out.Input = input

	for _, t := range req.Tools {
		if t.Type != "function" {
			continue // built-in tool passthrough is out of scope for v1 (plan §2)
		}
		out.Tools = append(out.Tools, Tool{
			Type:        "function",
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
			Strict:      t.Function.Strict,
		})
	}

	if tc, ok := translateToolChoice(req.ToolChoice); ok {
		out.ToolChoice = tc
	}

	out.Reasoning = translateReasoning(req.ReasoningEffort)
	out.Text = translateResponseFormat(req.ResponseFormat)

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: marshal responses request: %w", err)
	}
	return body, nil
}

// translateChatMessages folds system/developer turns into instructions and
// converts the rest of the transcript into Responses input items. Prior
// reasoning items are NOT reconstructed (the gateway never sees encrypted
// reasoning content); each turn reasons fresh from the transcript, matching
// today's chat-completions behavior (plan §4.3).
func translateChatMessages(msgs []chatReqMessage) (string, []any, error) {
	var sysParts []string
	var input []any

	for _, m := range msgs {
		switch m.Role {
		case "system", "developer":
			if text, ok := flattenContent(m.Content); ok && text != "" {
				sysParts = append(sysParts, text)
			}
		case "user":
			parts, err := translateUserContent(m.Content)
			if err != nil {
				return "", nil, err
			}
			if len(parts) > 0 {
				input = append(input, messageItem{Type: "message", Role: "user", Content: parts})
			}
		case "assistant":
			if text, ok := flattenContent(m.Content); ok && text != "" {
				input = append(input, messageItem{
					Type:    "message",
					Role:    "assistant",
					Content: []contentPart{{Type: "output_text", Text: text}},
				})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Function.Arguments
				if args == "" {
					args = "{}"
				}
				input = append(input, functionCallItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				})
			}
		case "tool":
			output, _ := flattenContent(m.Content)
			input = append(input, functionCallOutputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: output,
			})
		}
	}

	return strings.Join(sysParts, "\n\n"), input, nil
}

// translateUserContent converts a user message content (string or part array)
// into Responses input parts. Non-text/image parts (audio) are dropped: the
// Responses re-route only triggers for tools+reasoning requests, which are
// text/vision workloads.
func translateUserContent(raw json.RawMessage) ([]contentPart, error) {
	if s, ok := asJSONString(raw); ok {
		return []contentPart{{Type: "input_text", Text: s}}, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var parts []chatContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid message content: %w", err)
	}
	var out []contentPart
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, contentPart{Type: "input_text", Text: p.Text})
		case "image_url":
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				out = append(out, contentPart{Type: "input_image", ImageURL: p.ImageURL.URL})
			}
		}
	}
	return out, nil
}

// flattenContent extracts plain text from a message content field: a JSON
// string verbatim, or the concatenated text parts of an array. ok is false
// when the field is absent/null.
func flattenContent(raw json.RawMessage) (string, bool) {
	if s, ok := asJSONString(raw); ok {
		return s, true
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var parts []chatContentPart
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), true
}

// asJSONString returns the value when raw is a JSON string literal.
func asJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

// translateToolChoice maps the chat tool_choice union onto the Responses one:
// the string modes are shared verbatim; the named-function object flattens
// from {type:"function",function:{name}} to {type:"function",name}.
func translateToolChoice(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	if s, ok := asJSONString(raw); ok {
		switch s {
		case "auto", "none", "required":
			return s, true
		}
		return nil, false
	}
	var tc struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &tc) != nil {
		return nil, false
	}
	if tc.Type == "function" && tc.Function.Name != "" {
		return map[string]string{"type": "function", "name": tc.Function.Name}, true
	}
	return nil, false
}

// translateReasoning maps reasoning_effort to the Responses reasoning config.
// summary:"auto" is requested whenever reasoning is on, so the model's
// reasoning summary streams back and can be surfaced as reasoning_content.
// An absent effort keeps the model default (these models reason by default)
// but still asks for the summary.
func translateReasoning(effort string) *Reasoning {
	if effort == "none" {
		return &Reasoning{Effort: "none"}
	}
	return &Reasoning{Effort: effort, Summary: "auto"}
}

// translateResponseFormat maps chat response_format to Responses text.format:
// json_object passes through; json_schema flattens its nested json_schema
// object to the top level. type:"text" (or anything unrecognized) omits the
// config, keeping the model default.
func translateResponseFormat(raw json.RawMessage) *TextConfig {
	if len(raw) == 0 {
		return nil
	}
	var rf struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Schema      json.RawMessage `json:"schema"`
			Strict      *bool           `json:"strict"`
		} `json:"json_schema"`
	}
	if json.Unmarshal(raw, &rf) != nil {
		return nil
	}
	switch rf.Type {
	case "json_object":
		return &TextConfig{Format: json.RawMessage(`{"type":"json_object"}`)}
	case "json_schema":
		format := map[string]any{
			"type": "json_schema",
			"name": rf.JSONSchema.Name,
		}
		if rf.JSONSchema.Description != "" {
			format["description"] = rf.JSONSchema.Description
		}
		if len(rf.JSONSchema.Schema) > 0 {
			format["schema"] = json.RawMessage(rf.JSONSchema.Schema)
		}
		if rf.JSONSchema.Strict != nil {
			format["strict"] = *rf.JSONSchema.Strict
		}
		if b, err := json.Marshal(format); err == nil {
			return &TextConfig{Format: b}
		}
	}
	return nil
}
