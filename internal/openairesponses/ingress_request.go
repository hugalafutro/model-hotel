package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The inbound direction: a client speaks the Responses API to POST
// /v1/responses and the gateway translates the request into the chat-completions
// shape the pipeline runs, so a hotel/ group can serve it from any provider. The
// request is stateless by construction: store is always false, and anything
// that needs server-side state (previous_response_id, item references, hosted
// tools) is refused up front with a message naming the field.

// RejectedRequest is a request the ingress cannot serve on any route. Field is
// the request member that caused it.
type RejectedRequest struct {
	Field  string
	Reason string
}

func (e *RejectedRequest) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

func reject(field, reason string) error {
	return &RejectedRequest{Field: field, Reason: reason}
}

// ingressRequest is the subset of a Responses request the ingress reads.
type ingressRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`        // string or []item
	Instructions       json.RawMessage   `json:"instructions"` // string, or items in newer spellings
	Tools              []json.RawMessage `json:"tools"`
	ToolChoice         json.RawMessage   `json:"tool_choice"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls"`
	MaxOutputTokens    int               `json:"max_output_tokens"`
	Reasoning          *Reasoning        `json:"reasoning"`
	Text               *ingressText      `json:"text"`
	Temperature        *float64          `json:"temperature"`
	TopP               *float64          `json:"top_p"`
	Metadata           json.RawMessage   `json:"metadata"`
	Store              *bool             `json:"store"`
	Stream             bool              `json:"stream"`
	PreviousResponseID json.RawMessage   `json:"previous_response_id"`
	Conversation       json.RawMessage   `json:"conversation"`
	Background         bool              `json:"background"`
}

type ingressText struct {
	Format json.RawMessage `json:"format"`
}

// inputItem is the union of every input item the ingress reads. A message may
// arrive without a type (the EasyInputMessage spelling: role + content).
type inputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	// function_call; Namespace is set on a call to a tool inside a namespace
	// tool, which a chat provider knows by its flat name.
	CallID    string             `json:"call_id"`
	Name      string             `json:"name"`
	Namespace string             `json:"namespace"`
	Arguments util.ToolArguments `json:"arguments"`
	// function_call_output: string or []content part
	Output json.RawMessage `json:"output"`
}

// inputPart is one typed part of a message item's content.
type inputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail"`
	FileID   string `json:"file_id"`
	FileURL  string `json:"file_url"`
	FileData string `json:"file_data"`
	Filename string `json:"filename"`
	Refusal  string `json:"refusal"`
}

// --- chat-completions request shape this ingress emits ---

type chatOutRequest struct {
	Model             string           `json:"model"`
	Messages          []chatOutMessage `json:"messages"`
	Tools             []chatReqTool    `json:"tools,omitempty"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	MaxTokens         int              `json:"max_tokens,omitempty"`
	ReasoningEffort   string           `json:"reasoning_effort,omitempty"`
	ResponseFormat    json.RawMessage  `json:"response_format,omitempty"`
	Temperature       *float64         `json:"temperature,omitempty"`
	TopP              *float64         `json:"top_p,omitempty"`
	Metadata          json.RawMessage  `json:"metadata,omitempty"`
	Stream            bool             `json:"stream,omitempty"`
}

// chatOutMessage is one chat message. Content is a string, a part array, or
// nil for a tool-call-only assistant turn.
type chatOutMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatOutPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *chatOutImageURL `json:"image_url,omitempty"`
	File     *chatOutFile     `json:"file,omitempty"`
}

type chatOutImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatOutFile struct {
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data"`
}

// TranslatedRequest is the chat request an ingress request became.
type TranslatedRequest struct {
	ChatBody []byte
	// Model is the client's model string verbatim, for provider/hotel routing.
	Model  string
	Stream bool
	// Facts is what the answer's Response object needs from the request.
	Facts *RequestFacts
	// NativeOnly is set when the request carries something only OpenAI's own
	// /v1/responses can serve (a hosted or custom tool, a file id, an audio
	// part): the chat translation left it out, so the request may only be
	// served natively. The handler refuses it once it knows a candidate would
	// be translated. It names the first such member.
	NativeOnly *RejectedRequest
}

// translation is the state of one request's translation: the first
// native-only member seen, if any.
type translation struct {
	nativeOnly *RejectedRequest
}

// deferNative records a member the chat translation cannot carry but OpenAI's
// own endpoint can, and lets the translation go on without it.
func (t *translation) deferNative(field, reason string) {
	if t.nativeOnly == nil {
		t.nativeOnly = &RejectedRequest{Field: field, Reason: reason}
	}
}

// RequestFacts is what the response side needs from the request: the tool
// name map, and the members a Response object echoes back to the client.
type RequestFacts struct {
	// ToolNames maps a chat function name back to the namespaced tool it
	// stands for, for every tool that arrived inside a namespace. Nil when
	// the request carried none.
	ToolNames         ToolNames
	Instructions      string
	Temperature       *float64
	TopP              *float64
	ParallelToolCalls *bool
	ToolChoice        json.RawMessage
	Metadata          json.RawMessage
	TextFormat        json.RawMessage
	Reasoning         *Reasoning
}

// ToolNames is the chat-name to namespaced-name map of one request.
type ToolNames map[string]NamespacedTool

// NamespacedTool is a function tool as it lives inside a Responses namespace
// tool: the client addresses it by namespace and name together.
type NamespacedTool struct {
	Namespace string
	Name      string
}

// chatName is the flat function name a chat provider sees for a namespaced
// tool, and the key ToolNames reverses it by. The namespace prefix keeps two
// namespaces' same-named tools apart; the map, not the spelling, is what maps
// a call back, so a name that happens to contain the separator is safe.
func (t NamespacedTool) chatName() string { return t.Namespace + "__" + t.Name }

// TranslateRequestToChat converts a Responses API request body into a
// chat-completions request body. A request that no route can serve returns a
// *RejectedRequest naming the field.
func TranslateRequestToChat(body []byte) (*TranslatedRequest, error) {
	var req ingressRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid request body: %s", jsonfault.Describe(err, len(body)))
	}
	if req.Model == "" {
		return nil, reject("model", "is required")
	}
	if err := rejectStateful(&req); err != nil {
		return nil, err
	}
	var tr translation

	out := chatOutRequest{
		Model:             req.Model,
		ParallelToolCalls: req.ParallelToolCalls,
		MaxTokens:         req.MaxOutputTokens,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		Metadata:          req.Metadata,
		Stream:            req.Stream,
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if req.Text != nil {
		out.ResponseFormat = translateTextFormat(req.Text.Format)
	}

	if text := flattenItemText(req.Instructions); text != "" {
		out.Messages = append(out.Messages, chatOutMessage{Role: "system", Content: text})
	}
	msgs, err := tr.translateInput(req.Input)
	if err != nil {
		return nil, err
	}
	out.Messages = append(out.Messages, msgs...)
	if out.Messages == nil {
		out.Messages = []chatOutMessage{}
	}

	tools, names, plain, err := tr.translateIngressTools(req.Tools)
	if err != nil {
		return nil, err
	}
	out.Tools = tools
	tc, err := tr.translateIngressToolChoice(req.ToolChoice, names, plain)
	if err != nil {
		return nil, err
	}
	out.ToolChoice = tc

	chatBody, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: marshal chat request: %w", err)
	}
	facts := &RequestFacts{
		ToolNames:         names,
		Instructions:      flattenItemText(req.Instructions),
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		ParallelToolCalls: req.ParallelToolCalls,
		ToolChoice:        req.ToolChoice,
		Metadata:          req.Metadata,
		Reasoning:         req.Reasoning,
	}
	if req.Text != nil {
		facts.TextFormat = req.Text.Format
	}
	return &TranslatedRequest{ChatBody: chatBody, Model: req.Model, Stream: req.Stream, Facts: facts, NativeOnly: tr.nativeOnly}, nil
}

// rejectStateful refuses the members that need state this gateway does not
// keep. Every /v1/responses call is stateless: the client re-sends the whole
// transcript each turn, as Codex does with store:false.
func rejectStateful(req *ingressRequest) error {
	const stateless = "not supported: this gateway keeps no conversation state, re-send the transcript in input"
	if util.ValueCarries(req.PreviousResponseID) {
		return reject("previous_response_id", stateless)
	}
	if util.ValueCarries(req.Conversation) {
		return reject("conversation", stateless)
	}
	if req.Store != nil && *req.Store {
		return reject("store", "must be false: this gateway keeps no conversation state")
	}
	if req.Background {
		return reject("background", "not supported: responses are served inline")
	}
	return nil
}

// translateInput converts the input member (a string, or a list of items)
// into chat messages. Consecutive function_call items, and an assistant text
// that precedes them, fold into one assistant message carrying tool_calls, the
// way chat-completions expresses a tool-calling turn.
func (t *translation) translateInput(raw json.RawMessage) ([]chatOutMessage, error) {
	if s, ok := egress.AsJSONString(raw); ok {
		return []chatOutMessage{{Role: "user", Content: s}}, nil
	}
	if !util.JSONMemberSet(raw) {
		return nil, nil
	}
	var items []inputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid input: %s", jsonfault.Describe(err, len(raw)))
	}
	var out []chatOutMessage
	for i, it := range items {
		field := fmt.Sprintf("input[%d]", i)
		kind := it.Type
		if kind == "" && it.Role != "" {
			kind = "message"
		}
		switch kind {
		case "message":
			m, err := t.translateInputMessage(it, field)
			if err != nil {
				return nil, err
			}
			if m != nil {
				out = append(out, *m)
			}
		case "function_call":
			name := it.Name
			if it.Namespace != "" {
				name = NamespacedTool{Namespace: it.Namespace, Name: it.Name}.chatName()
			}
			call := chatToolCall{
				ID:       it.CallID,
				Type:     "function",
				Function: chatToolCallFunc{Name: name, Arguments: it.Arguments},
			}
			if n := len(out); n > 0 && out[n-1].Role == "assistant" && out[n-1].ToolCallID == "" {
				out[n-1].ToolCalls = append(out[n-1].ToolCalls, call)
			} else {
				out = append(out, chatOutMessage{Role: "assistant", Content: nil, ToolCalls: []chatToolCall{call}})
			}
		case "function_call_output":
			out = append(out, chatOutMessage{Role: "tool", ToolCallID: it.CallID, Content: flattenOutput(it.Output)})
		case "reasoning":
			// Encrypted or summarised reasoning from an earlier turn: nothing a
			// chat provider can replay. The model reasons fresh from the
			// transcript, the same choice the egress direction makes.
		case "item_reference", "compaction", "context_compaction":
			return nil, reject(field, kind+" refers to server-side state this gateway does not keep")
		default:
			// A hosted tool's call or output from an earlier turn: only the
			// endpoint that ran the tool can read it back.
			t.deferNative(field, "item type "+kind+" is served by OpenAI's /v1/responses only")
		}
	}
	return out, nil
}

// translateInputMessage converts one message item. Assistant content becomes
// plain text (refusals dropped); user, system and developer content keeps its
// text, image and inline file parts. developer becomes system: the OpenAI-only
// role is what the chat providers behind a hotel/ group reject.
func (t *translation) translateInputMessage(it inputItem, field string) (*chatOutMessage, error) {
	role := it.Role
	switch role {
	case "developer":
		role = "system"
	case "user", "assistant", "system":
	default:
		return nil, reject(field, "role "+role+" is not a message role: use user, assistant, system or developer")
	}
	if role == "assistant" || role == "system" {
		text := flattenItemText(it.Content)
		if text == "" {
			return nil, nil
		}
		return &chatOutMessage{Role: role, Content: text}, nil
	}
	if !util.JSONMemberSet(it.Content) {
		return nil, nil
	}
	if s, ok := egress.AsJSONString(it.Content); ok {
		return &chatOutMessage{Role: "user", Content: s}, nil
	}
	var parts []inputPart
	if err := json.Unmarshal(it.Content, &parts); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid %s.content: %s", field, jsonfault.Describe(err, len(it.Content)))
	}
	var out []chatOutPart
	for j, p := range parts {
		partField := fmt.Sprintf("%s.content[%d]", field, j)
		switch p.Type {
		case "input_text", "text":
			out = append(out, chatOutPart{Type: "text", Text: p.Text})
		case "input_image":
			if p.ImageURL == "" {
				t.deferNative(partField, "input_image by file id is served by OpenAI's /v1/responses only; other routes need an inline image_url")
				continue
			}
			out = append(out, chatOutPart{Type: "image_url", ImageURL: &chatOutImageURL{URL: p.ImageURL, Detail: p.Detail}})
		case "input_file":
			if p.FileData == "" {
				t.deferNative(partField, "input_file by file id or url is served by OpenAI's /v1/responses only; other routes need inline file_data")
				continue
			}
			out = append(out, chatOutPart{Type: "file", File: &chatOutFile{Filename: p.Filename, FileData: p.FileData}})
		default:
			t.deferNative(partField, "content part type "+p.Type+" is served by OpenAI's /v1/responses only")
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &chatOutMessage{Role: "user", Content: out}, nil
}

// flattenItemText reduces a Responses content member to plain text: a string
// verbatim, or the text parts of an item content array (input_text,
// output_text and the bare text spelling) joined by newlines, since each part
// is its own block and Codex sends a developer turn as several. Absent, null
// and unreadable all yield "". Distinct from egress.FlattenText, which reads
// the chat part vocabulary.
func flattenItemText(raw json.RawMessage) string {
	if !util.JSONMemberSet(raw) {
		return ""
	}
	if s, ok := egress.AsJSONString(raw); ok {
		return s
	}
	var parts []inputPart
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		switch p.Type {
		case "input_text", "output_text", "text", "":
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// flattenOutput renders a function_call_output's output as the string a chat
// tool message carries: a string verbatim, a content array as its text parts
// joined by newlines.
func flattenOutput(raw json.RawMessage) string {
	if s, ok := egress.AsJSONString(raw); ok {
		return s
	}
	var parts []inputPart
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ingressTool is one entry of tools, or of a namespace tool's own list.
type ingressTool struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  json.RawMessage   `json:"parameters"`
	Strict      *bool             `json:"strict"`
	Tools       []json.RawMessage `json:"tools"` // namespace
}

func (t ingressTool) chatTool(name string) chatReqTool {
	return chatReqTool{Type: "function", Function: chatReqToolFunc{
		Name:        name,
		Description: t.Description,
		Parameters:  t.Parameters,
		Strict:      t.Strict,
	}}
}

// translateIngressTools maps function tools onto the chat shape. A namespace
// tool (how Codex groups an MCP server's tools) is flattened into the function
// tools it holds, under namespaced chat names that ToolNames reverses on the
// answer. A hosted web search is dropped rather than refused: Codex advertises
// one to every custom provider by default, and a provider behind a hotel/
// group simply has no search, as it would not on any other gateway. Every
// other hosted or custom tool needs execution or state this route does not
// have, so it is recorded as native-only and left out of the chat request.
// plain is the set of top-level function names; every chat name, plain or
// generated, must be unique, or a call could not be mapped back.
func (t *translation) translateIngressTools(raw []json.RawMessage) (out []chatReqTool, names ToolNames, plain map[string]bool, err error) {
	names = ToolNames{}
	plain = map[string]bool{}
	seen := map[string]string{} // chat name -> the tools[...] field that claimed it
	claim := func(field, name string) error {
		if prev, dup := seen[name]; dup {
			return reject(field, "tool name "+name+" collides with "+prev+": every tool needs a distinct name, namespaces included")
		}
		seen[name] = field
		return nil
	}
	for i, r := range raw {
		field := fmt.Sprintf("tools[%d]", i)
		var tool ingressTool
		if err := json.Unmarshal(r, &tool); err != nil {
			return nil, nil, nil, fmt.Errorf("openairesponses: invalid %s: %s", field, jsonfault.Describe(err, len(r)))
		}
		switch {
		case tool.Type == "function":
			if err := claim(field, tool.Name); err != nil {
				return nil, nil, nil, err
			}
			plain[tool.Name] = true
			out = append(out, tool.chatTool(tool.Name))
		case tool.Type == "namespace":
			for j, ir := range tool.Tools {
				innerField := fmt.Sprintf("%s.tools[%d]", field, j)
				var inner ingressTool
				if err := json.Unmarshal(ir, &inner); err != nil {
					return nil, nil, nil, fmt.Errorf("openairesponses: invalid %s: %s", innerField, jsonfault.Describe(err, len(ir)))
				}
				if inner.Type != "function" {
					t.deferNative(innerField, "tool type "+inner.Type+" is served by OpenAI's /v1/responses only; other routes take function tools")
					continue
				}
				nt := NamespacedTool{Namespace: tool.Name, Name: inner.Name}
				if err := claim(innerField, nt.chatName()); err != nil {
					return nil, nil, nil, err
				}
				names[nt.chatName()] = nt
				out = append(out, inner.chatTool(nt.chatName()))
			}
		case strings.HasPrefix(tool.Type, "web_search"):
			// dropped
		default:
			t.deferNative(field, "tool type "+tool.Type+" is served by OpenAI's /v1/responses only; other routes take function tools")
		}
	}
	if len(names) == 0 {
		names = nil
	}
	return out, names, plain, nil
}

// translateIngressToolChoice maps the Responses tool_choice onto the chat one:
// the string modes verbatim, a named function nested under "function". A name
// that is no top-level tool but names exactly one namespaced tool is mapped to
// that tool's chat name, so the provider is asked for a tool it was given; a
// name that matches nothing, or several namespaces, is refused here rather
// than as the provider's 400 for a tool it never received.
func (t *translation) translateIngressToolChoice(raw json.RawMessage, names ToolNames, plain map[string]bool) (any, error) {
	if !util.JSONMemberSet(raw) {
		return nil, nil
	}
	if s, ok := egress.AsJSONString(raw); ok {
		return s, nil
	}
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid tool_choice: %s", jsonfault.Describe(err, len(raw)))
	}
	if tc.Type != "function" || tc.Name == "" {
		t.deferNative("tool_choice", "type "+tc.Type+" is served by OpenAI's /v1/responses only; other routes take a mode string or a named function")
		return nil, nil
	}
	name := tc.Name
	if !plain[name] {
		var matches []string
		for chatName, nt := range names {
			if nt.Name == name {
				matches = append(matches, chatName)
			}
		}
		switch len(matches) {
		case 1:
			name = matches[0]
		case 0:
			return nil, reject("tool_choice", "names a tool the request does not define")
		default:
			return nil, reject("tool_choice", "names a tool that several namespaces define; the request cannot say which")
		}
	}
	return map[string]any{"type": "function", "function": map[string]string{"name": name}}, nil
}

// translateTextFormat is the inverse of translateResponseFormat: text.format
// becomes chat response_format. A text format, or one this gateway does not
// know, keeps the model default.
func translateTextFormat(raw json.RawMessage) json.RawMessage {
	if !util.JSONMemberSet(raw) {
		return nil
	}
	var f struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Schema      json.RawMessage `json:"schema"`
		Strict      *bool           `json:"strict"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	switch f.Type {
	case "json_object":
		return json.RawMessage(`{"type":"json_object"}`)
	case "json_schema":
		schema := map[string]any{"name": f.Name}
		if f.Description != "" {
			schema["description"] = f.Description
		}
		if util.JSONMemberSet(f.Schema) {
			schema["schema"] = f.Schema
		}
		if f.Strict != nil {
			schema["strict"] = *f.Strict
		}
		b, err := json.Marshal(map[string]any{"type": "json_schema", "json_schema": schema})
		if err != nil {
			return nil
		}
		return b
	}
	return nil
}
