package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Incoming Anthropic Messages request shape ---
//
// We decode only the fields the gateway needs to translate. Unknown fields are
// ignored; provider-specific knobs the proxy already handles (and any params an
// upstream rejects) are dropped here or stripped by the proxy's 400 auto-retry.

// Request is a decoded Anthropic Messages API request.
type Request struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Messages      []ReqMessage    `json:"messages"`
	System        json.RawMessage `json:"system"` // string OR []contentBlock
	Stream        bool            `json:"stream"`
	Temperature   *float64        `json:"temperature"`
	TopP          *float64        `json:"top_p"`
	TopK          *int            `json:"top_k"`
	StopSequences []string        `json:"stop_sequences"`
	Tools         []ReqTool       `json:"tools"`
	ToolChoice    json.RawMessage `json:"tool_choice"`
}

// ReqMessage is one message in the conversation. Content is a string or an
// array of typed blocks.
type ReqMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ReqTool is one Anthropic tool definition.
type ReqTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// reqBlock is one content block within a message (or system) array.
type reqBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text"`
	// image / document source
	Source *blockSource `json:"source"`
	// tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // string OR []reqBlock
	IsError   bool            `json:"is_error"`
}

type blockSource struct {
	Type      string `json:"type"`       // "base64" | "url"
	MediaType string `json:"media_type"` // for base64
	Data      string `json:"data"`       // for base64
	URL       string `json:"url"`        // for url
}

// --- Outgoing OpenAI chat-completions request shape ---

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	TopK        *int         `json:"top_k,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	ToolChoice  any          `json:"tool_choice,omitempty"`
}

// oaiMessage uses `any` for Content because OpenAI accepts either a plain string
// or an array of content parts; Content is left nil (omitted) for tool-call-only
// assistant turns.
type oaiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiContentPart struct {
	Type     string       `json:"type"` // "text" | "image_url"
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // "function"
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string      `json:"type"` // "function"
	Function oaiToolFunc `json:"function"`
}

type oaiToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// TranslateRequest converts an Anthropic Messages request body into an
// OpenAI chat-completions request body. It returns the OpenAI JSON, the model
// string (verbatim, for the proxy's existing provider/hotel routing), and the
// stream flag. The translation is lossy by design for v1 (thinking blocks and
// cache_control are dropped on this path; see plan), but preserves text, vision,
// tool definitions, tool calls, and tool results.
func TranslateRequest(body []byte) (openaiBody []byte, model string, stream bool, err error) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", false, fmt.Errorf("anthropic: invalid request body: %w", err)
	}
	if req.Model == "" {
		return nil, "", false, fmt.Errorf("anthropic: model is required")
	}

	out := oaiRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
		Stop:        req.StopSequences,
	}

	// system -> leading system message.
	if len(req.System) > 0 {
		if sys, ok := decodeText(req.System); ok && sys != "" {
			out.Messages = append(out.Messages, oaiMessage{Role: "system", Content: sys})
		}
	}

	for _, m := range req.Messages {
		msgs, err := translateMessage(m)
		if err != nil {
			return nil, "", false, err
		}
		out.Messages = append(out.Messages, msgs...)
	}

	// tools.
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, oaiTool{
			Type: "function",
			Function: oaiToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// tool_choice.
	if tc, ok := translateToolChoice(req.ToolChoice); ok {
		out.ToolChoice = tc
	}

	openaiBody, err = json.Marshal(out)
	if err != nil {
		return nil, "", false, fmt.Errorf("anthropic: marshal openai request: %w", err)
	}
	return openaiBody, req.Model, req.Stream, nil
}

// translateMessage converts one Anthropic message into one or more OpenAI
// messages. A user turn carrying tool_result blocks expands into separate
// role:"tool" messages (plus a user message for any remaining text/image), and
// an assistant turn carrying tool_use blocks collapses into a single assistant
// message with tool_calls.
func translateMessage(m ReqMessage) ([]oaiMessage, error) {
	// Plain string content: straight passthrough. Only a genuine JSON string
	// short-circuits here — an array of blocks must fall through to block
	// handling, or non-text blocks (images, tool_use, tool_result) are dropped.
	if s, ok := asJSONString(m.Content); ok {
		return []oaiMessage{{Role: m.Role, Content: s}}, nil
	}

	var blocks []reqBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil, fmt.Errorf("anthropic: invalid message content: %w", err)
	}

	var out []oaiMessage
	var parts []oaiContentPart
	var toolCalls []oaiToolCall

	flushUserParts := func() {
		if len(parts) == 0 {
			return
		}
		out = append(out, oaiMessage{Role: m.Role, Content: parts})
		parts = nil
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, oaiContentPart{Type: "text", Text: b.Text})
		case "image":
			if url, ok := imageURL(b.Source); ok {
				parts = append(parts, oaiContentPart{Type: "image_url", ImageURL: &oaiImageURL{URL: url}})
			}
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, oaiToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: oaiToolCallFunc{Name: b.Name, Arguments: args},
			})
		case "tool_result":
			// Tool results become standalone role:"tool" messages. Emit any
			// pending user parts first so ordering is preserved.
			flushUserParts()
			content, _ := decodeToolResultContent(b.Content)
			out = append(out, oaiMessage{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    content,
			})
		case "document", "thinking", "redacted_thinking":
			// v1: best-effort drop. Documents have no clean OpenAI equivalent;
			// thinking is preserved only on the native passthrough path.
		}
	}

	if len(toolCalls) > 0 {
		// Assistant tool-call turn: one message carrying the text (if any) and
		// the collected tool_calls. OpenAI wants content as a string here.
		var content any
		if text := joinTextParts(parts); text != "" {
			content = text
		}
		out = append(out, oaiMessage{Role: m.Role, Content: content, ToolCalls: toolCalls})
		parts = nil
	} else {
		flushUserParts()
	}

	return out, nil
}

// asJSONString returns the value when raw is a JSON string literal, ok=false
// otherwise (including arrays/objects). Used to distinguish plain-string message
// content from a content-block array.
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

// decodeText returns the string when raw is a JSON string, or the concatenation
// of the text blocks when raw is a content-block array. ok is false when raw is
// neither (e.g. an empty/absent field). Used for fields where flattening to text
// is correct (system prompt, tool_result content).
func decodeText(raw json.RawMessage) (string, bool) {
	if s, ok := asJSONString(raw); ok {
		return s, true
	}
	var blocks []reqBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return joinReqTextBlocks(blocks), true
	}
	return "", false
}

// decodeToolResultContent flattens a tool_result content field (string or block
// array) into the text OpenAI expects on a role:"tool" message.
func decodeToolResultContent(raw json.RawMessage) (string, bool) {
	return decodeText(raw)
}

func joinReqTextBlocks(blocks []reqBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func joinTextParts(parts []oaiContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// imageURL builds the OpenAI image_url value from an Anthropic image source:
// a data: URI for base64 sources, or the URL verbatim for url sources.
func imageURL(src *blockSource) (string, bool) {
	if src == nil {
		return "", false
	}
	switch src.Type {
	case "base64":
		if src.Data == "" {
			return "", false
		}
		return fmt.Sprintf("data:%s;base64,%s", src.MediaType, src.Data), true
	case "url":
		if src.URL == "" {
			return "", false
		}
		return src.URL, true
	}
	return "", false
}

// translateToolChoice maps the Anthropic tool_choice union to the OpenAI form.
func translateToolChoice(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &tc) != nil {
		return nil, false
	}
	switch tc.Type {
	case "auto":
		return "auto", true
	case "none":
		// The caller explicitly prohibits tool use; OpenAI's equivalent is the
		// literal "none". Dropping this would let the upstream default to "auto"
		// and call a tool against the caller's intent.
		return "none", true
	case "any":
		return "required", true
	case "tool":
		if tc.Name == "" {
			return "required", true
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		}, true
	}
	return nil, false
}
