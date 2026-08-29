// Package anthropicegress translates between the OpenAI chat-completions wire
// shape and Anthropic's native Messages shape. It is an *egress* dialect
// adapter (the mirror image of internal/anthropic, which translates on
// ingress): Anthropic's OpenAI-compat /v1/chat/completions endpoint cannot
// carry a document — a request holding an OpenAI file content part comes back
// as "messages.0.user.content.str: Input should be a valid string" — so a
// request carrying one is rewritten here and sent to /v1/messages instead.
package anthropicegress

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// DefaultMaxTokens is the max_tokens supplied when the caller sends none.
// Anthropic requires the field, and a request that omits it 400s rather than
// falling back to a model default.
const DefaultMaxTokens = 4096

// documentMediaType is the media type stamped on a base64 document source.
// Anthropic's base64 document source accepts application/pdf only, and the
// media type on the way in is unreliable — clients hand us PDFs labelled
// application/octet-stream, or unlabelled inside an image part.
const documentMediaType = "application/pdf"

// thinkingBudgets maps OpenAI reasoning_effort to an Anthropic thinking budget
// (tokens), for models on the budget dialect. An effort outside this map —
// "none", absent — leaves the thinking block off entirely; Anthropic's minimum
// budget is 1024, so there is no budget that expresses "reason a little".
// "minimal" is OpenAI's floor and maps to Anthropic's.
var thinkingBudgets = map[string]int{
	"minimal": 1024,
	"low":     1024,
	"medium":  4096,
	"high":    8192,
}

// thinkingEfforts maps OpenAI reasoning_effort to Anthropic's own effort scale,
// for models on the adaptive dialect. Anthropic accepts low, medium, high,
// xhigh and max; the three shared words pass through unchanged, OpenAI's
// "minimal" floors to "low", and Anthropic's two extra levels are honoured for a
// client that asks for them by name. An effort outside this map leaves thinking
// off, matching thinkingBudgets.
var thinkingEfforts = map[string]string{
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "xhigh",
	"max":     "max",
}

// ThinkingDialect is how a model wants extended thinking asked for. The two
// shapes are mutually exclusive and a model accepts one, the other, or both,
// with no way to tell from the model id — Anthropic moved from one to the other
// mid-generation, and a Messages endpoint that is not Anthropic's may serve
// anything. Live behaviour, 2026-08-20:
//
//	claude-opus-5, claude-sonnet-5    adaptive only
//	claude-sonnet-4-6                 both
//	claude-opus-4-5, claude-haiku-4-5 budget only
//
// So the dialect is a per-model fact to be learned from the upstream's own 400,
// not derived. See DialectFromError.
type ThinkingDialect int

const (
	// ThinkingAdaptive asks with `thinking: {type: "adaptive"}` plus
	// `output_config: {effort}`, letting the model choose how much to think
	// within the effort ceiling. The default: it is what current models take,
	// and the only shape the newest ones accept.
	ThinkingAdaptive ThinkingDialect = iota
	// ThinkingBudget asks with `thinking: {type: "enabled", budget_tokens}`,
	// the older shape, which the newest models reject outright.
	ThinkingBudget
)

// String names the dialect for logs.
func (d ThinkingDialect) String() string {
	if d == ThinkingBudget {
		return "budget"
	}
	return "adaptive"
}

// --- Incoming OpenAI chat-completions request shape ---
//
// Only the fields the translation needs are decoded. Params with no Anthropic
// equivalent (frequency_penalty, presence_penalty, n, seed, response_format,
// logprobs, stream_options, ...) are dropped by omission.

type oaiRequest struct {
	Model               string          `json:"model"`
	Messages            []oaiMessage    `json:"messages"`
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Stream              bool            `json:"stream"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	TopK                *int            `json:"top_k"`
	Stop                json.RawMessage `json:"stop"` // string OR []string
	Tools               []oaiTool       `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ReasoningEffort     string          `json:"reasoning_effort"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []oaiContentPart OR null
	ToolCalls  []oaiToolCall   `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type oaiContentPart struct {
	Type     string `json:"type"` // "text" | "image_url" | "file"
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
	File *oaiFile `json:"file"`
}

type oaiFile struct {
	FileData string `json:"file_data"` // data: URI
	FileURL  string `json:"file_url"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name string `json:"name"`
		// util.ToolArguments, not a plain string: the spec says a JSON string and
		// several providers send the object, so #808 taught the RESPONSE side to
		// accept both — which means the caller receives the object form, echoes
		// the assistant turn back in its next request, and this decoder rejected
		// what the gateway itself had handed it. In a failover group whose next
		// turn lands here, that 400s for the life of the conversation.
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

// --- Outgoing Anthropic Messages request shape ---

type antRequest struct {
	Model         string           `json:"model"`
	MaxTokens     int              `json:"max_tokens"`
	Messages      []antMessage     `json:"messages"`
	System        string           `json:"system,omitempty"`
	Stream        bool             `json:"stream,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	TopK          *int             `json:"top_k,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	Tools         []antTool        `json:"tools,omitempty"`
	ToolChoice    *antToolChoice   `json:"tool_choice,omitempty"`
	Thinking      *antThinking     `json:"thinking,omitempty"`
	OutputConfig  *antOutputConfig `json:"output_config,omitempty"`
}

// antOutputConfig carries the effort ceiling that accompanies adaptive
// thinking. A model on the budget dialect ignores it rather than rejecting it,
// so it rides along harmlessly if the dialects are ever confused.
type antOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// antMessage carries Content as any because Anthropic accepts either a plain
// string or an array of blocks, and a string round-trips a string-content
// message unchanged.
type antMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type antBlock struct {
	Type string `json:"type"` // "text" | "image" | "document" | "tool_use" | "tool_result"
	// text
	Text string `json:"text,omitempty"`
	// image / document
	Source *antSource `json:"source,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type antSource struct {
	Type      string `json:"type"` // "base64" | "url" | "text"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type antToolChoice struct {
	Type string `json:"type"` // "auto" | "any" | "tool"
	Name string `json:"name,omitempty"`
}

type antThinking struct {
	Type         string `json:"type"`                    // "enabled" | "adaptive"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // budget dialect only
}

// TranslateRequest converts an OpenAI chat-completions request body into an
// Anthropic Messages request body, asking for extended thinking in the default
// adaptive dialect. It returns the Anthropic JSON, the model string (verbatim —
// the body carries it, and the caller keeps it for routing and metering) and the
// stream flag.
//
// Callers that know a model wants the older budget dialect (the proxy learns
// this from a 400, see DialectFromError) use TranslateRequestWithDialect.
func TranslateRequest(chatBody []byte) (body []byte, model string, stream bool, err error) {
	return TranslateRequestWithDialect(chatBody, ThinkingAdaptive)
}

// TranslateRequestWithDialect is TranslateRequest with the thinking dialect
// chosen by the caller.
func TranslateRequestWithDialect(chatBody []byte, dialect ThinkingDialect) (body []byte, model string, stream bool, err error) {
	var req oaiRequest
	if err := json.Unmarshal(chatBody, &req); err != nil {
		return nil, "", false, fmt.Errorf("anthropicegress: invalid request body: %s", jsonfault.Describe(err, len(chatBody)))
	}
	if req.Model == "" {
		return nil, "", false, errors.New("anthropicegress: model is required")
	}

	out := antRequest{
		Model:         req.Model,
		Stream:        req.Stream,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: egress.DecodeStop(req.Stop),
	}

	system, messages, err := translateMessages(req.Messages)
	if err != nil {
		return nil, "", false, err
	}
	if len(messages) == 0 {
		return nil, "", false, errors.New("anthropicegress: at least one user or assistant message with content is required")
	}
	out.System = system
	out.Messages = messages

	// tool_choice "none" has no Anthropic equivalent; the tools array goes with
	// it, or the model would treat the request as tools-with-auto and call one
	// against the caller's intent.
	choice, keepTools := translateToolChoice(req.ToolChoice)
	out.ToolChoice = choice
	if keepTools {
		out.Tools = translateTools(req.Tools)
	}

	// max_completion_tokens is the modern OpenAI field and wins over the
	// deprecated max_tokens when both are present. A non-positive value is no
	// budget at all — Anthropic 400s on it — so it falls through to the default.
	out.MaxTokens = req.MaxCompletionTokens
	if out.MaxTokens <= 0 {
		out.MaxTokens = req.MaxTokens
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = DefaultMaxTokens
	}

	applyThinking(&out, req.ReasoningEffort, dialect)

	body, err = json.Marshal(out)
	if err != nil {
		return nil, "", false, fmt.Errorf("anthropicegress: marshal messages request: %w", err)
	}
	return body, req.Model, req.Stream, nil
}

// applyThinking turns an OpenAI reasoning_effort into the Anthropic thinking
// request the given dialect expects, and makes the rest of the body consistent
// with it. An effort the dialect has no mapping for leaves the request
// untouched, thinking off — the same outcome as sending no effort at all.
//
// Both dialects share one hard constraint, which is why this is not two
// unrelated branches: Anthropic rejects a temperature other than 1 whenever
// thinking is on, in either shape (`temperature may only be set to 1 when
// thinking is enabled or in adaptive mode`), and treats top_p/top_k the same
// way. Dropping the sampling knobs is the only way to honour the caller's
// reasoning request at all, and it is the lesser loss: a caller who asks to
// think is asking about reasoning depth, not sampling.
func applyThinking(out *antRequest, reasoningEffort string, dialect ThinkingDialect) {
	effort := strings.ToLower(strings.TrimSpace(reasoningEffort))

	switch dialect {
	case ThinkingBudget:
		budget, ok := thinkingBudgets[effort]
		if !ok {
			return
		}
		out.Thinking = &antThinking{Type: "enabled", BudgetTokens: budget}
		// Anthropic requires max_tokens strictly greater than the budget: the
		// remainder is the visible answer's allowance, so it gets a full default.
		if out.MaxTokens <= budget {
			out.MaxTokens = budget + DefaultMaxTokens
		}
	default: // ThinkingAdaptive
		level, ok := thinkingEfforts[effort]
		if !ok {
			return
		}
		// The model decides how much to think, up to this ceiling, so there is no
		// budget to keep max_tokens clear of. It still needs room for thinking AND
		// an answer, and a caller who sent a small max_tokens with a big effort
		// gets a stream of pure thinking cut off before any text (observed live:
		// max_tokens 30 spent 29 thinking tokens and returned no content). Raising
		// a too-small allowance is the difference between an answer and nothing.
		out.Thinking = &antThinking{Type: "adaptive"}
		out.OutputConfig = &antOutputConfig{Effort: level}
		if out.MaxTokens < minAdaptiveMaxTokens {
			out.MaxTokens = minAdaptiveMaxTokens
		}
	}
	out.Temperature, out.TopP, out.TopK = nil, nil, nil
}

// minAdaptiveMaxTokens is the allowance an adaptive-thinking request is raised
// to when the caller asked for less. It is DefaultMaxTokens, the same figure a
// caller who named no budget gets: enough that thinking cannot consume the whole
// allowance before the answer starts.
const minAdaptiveMaxTokens = DefaultMaxTokens

// translateMessages converts the OpenAI message list into the Anthropic system
// prompt plus the conversation turns. system/developer messages lift out of the
// turn list (Anthropic has no such role), a run of role:"tool" messages
// coalesces into one user turn, and adjacent same-role turns merge.
func translateMessages(in []oaiMessage) (string, []antMessage, error) {
	var systemParts []string
	var out []antMessage
	// Open run of tool_result blocks: Anthropic's turns must alternate and it
	// rejects a sequence of user turns each holding a single result, so
	// consecutive tool messages accumulate here and flush as one turn when the
	// run ends.
	var toolRun []antBlock
	flushToolRun := func() {
		if len(toolRun) > 0 {
			out = appendTurn(out, antMessage{Role: "user", Content: toolRun})
			toolRun = nil
		}
	}

	for _, m := range in {
		if m.Role == "tool" {
			toolRun = append(toolRun, antBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   flattenText(m.Content),
			})
			continue
		}
		flushToolRun()

		if m.Role == "system" || m.Role == "developer" {
			if text := flattenText(m.Content); text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}

		role := "user" // anything unrecognised is a user turn
		if m.Role == "assistant" {
			role = "assistant"
		}
		msg, err := translateTurn(role, m)
		if err != nil {
			return "", nil, err
		}
		if msg != nil {
			out = appendTurn(out, *msg)
		}
	}
	flushToolRun()

	return strings.Join(systemParts, "\n\n"), out, nil
}

// appendTurn adds a turn to the conversation, merging its content into the
// previous turn when the roles match. Anthropic's turns must alternate, and
// OpenAI accepts (and clients send) adjacent same-role messages.
func appendTurn(out []antMessage, m antMessage) []antMessage {
	if len(out) == 0 || out[len(out)-1].Role != m.Role {
		return append(out, m)
	}
	prev := &out[len(out)-1]
	prev.Content = append(contentBlocks(prev.Content), contentBlocks(m.Content)...)
	return out
}

// contentBlocks normalises a turn's content to blocks so two turns can merge:
// a plain string is promoted to a single text block.
func contentBlocks(content any) []antBlock {
	switch c := content.(type) {
	case []antBlock:
		return c
	case string:
		return []antBlock{{Type: "text", Text: c}}
	}
	return nil
}

// translateTurn builds one Anthropic turn from an OpenAI message. It returns
// nil when the message carries nothing Anthropic can hold (empty or null
// content and no tool calls).
func translateTurn(role string, m oaiMessage) (*antMessage, error) {
	// Plain string content stays a plain string — but only when nothing else
	// has to ride along in the same turn.
	if s, ok := egress.AsJSONString(m.Content); ok && len(m.ToolCalls) == 0 {
		if s == "" {
			return nil, nil
		}
		return &antMessage{Role: role, Content: s}, nil
	}

	blocks, err := translateBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	for _, tc := range m.ToolCalls {
		blocks = append(blocks, antBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: util.ToolArgumentsObject(tc.Function.Arguments),
		})
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return &antMessage{Role: role, Content: blocks}, nil
}

// translateBlocks converts an OpenAI message content field (string, part array
// or null) into Anthropic content blocks. Part types with no Anthropic
// equivalent are dropped rather than forwarded raw.
func translateBlocks(raw json.RawMessage) ([]antBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if s, ok := egress.AsJSONString(raw); ok {
		if s == "" {
			return nil, nil
		}
		return []antBlock{{Type: "text", Text: s}}, nil
	}

	var parts []oaiContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("anthropicegress: invalid message content: %s", jsonfault.Describe(err, len(raw)))
	}

	var blocks []antBlock
	for _, p := range parts {
		switch p.Type {
		case "text":
			// An empty text block marshals as {"type":"text"}, which Anthropic
			// rejects — drop it.
			if p.Text == "" {
				continue
			}
			blocks = append(blocks, antBlock{Type: "text", Text: p.Text})
		case "image_url":
			if p.ImageURL == nil || p.ImageURL.URL == "" {
				continue
			}
			if b, ok := imageBlock(p.ImageURL.URL); ok {
				blocks = append(blocks, b)
			}
		case "file":
			if p.File == nil {
				continue
			}
			if b, ok := fileBlock(p.File); ok {
				blocks = append(blocks, b)
			}
		}
	}
	return blocks, nil
}

// imageBlock maps an OpenAI image_url value onto an Anthropic block: a base64
// image source for a data: URI with an image media type, a url image source for
// an ordinary URL, and a document for a data: URI carrying anything else (which
// is how clients smuggle PDFs past the compat endpoint's file-part rejection).
func imageBlock(u string) (antBlock, bool) {
	du, ok := parseDataURI(u)
	if !ok {
		return antBlock{Type: "image", Source: &antSource{Type: "url", URL: u}}, true
	}
	if imageMediaTypes[du.mediaType] {
		data, ok := base64Payload(du)
		if !ok {
			return antBlock{}, false
		}
		return antBlock{Type: "image", Source: &antSource{
			Type:      "base64",
			MediaType: du.mediaType,
			Data:      data,
		}}, true
	}
	return documentBlock(du)
}

// fileBlock maps an OpenAI file part onto an Anthropic document block.
func fileBlock(f *oaiFile) (antBlock, bool) {
	if f.FileData != "" {
		du, ok := parseDataURI(f.FileData)
		if !ok {
			return antBlock{}, false
		}
		return documentBlock(du)
	}
	if f.FileURL != "" {
		return antBlock{Type: "document", Source: &antSource{Type: "url", URL: f.FileURL}}, true
	}
	return antBlock{}, false
}

// documentBlock builds a document block from a parsed data: URI. A text/plain
// payload becomes a text source carrying the decoded text; everything else
// becomes a base64 PDF source, the only binary document Anthropic accepts.
func documentBlock(du dataURI) (antBlock, bool) {
	if du.mediaType == "text/plain" {
		text, ok := decodePayload(du)
		if !ok || text == "" {
			return antBlock{}, false
		}
		return antBlock{Type: "document", Source: &antSource{
			Type:      "text",
			MediaType: "text/plain",
			Data:      text,
		}}, true
	}
	data, ok := base64Payload(du)
	if !ok {
		return antBlock{}, false
	}
	return antBlock{Type: "document", Source: &antSource{
		Type:      "base64",
		MediaType: documentMediaType,
		Data:      data,
	}}, true
}

// base64Payload returns a data: URI payload in the base64 an Anthropic base64
// source expects. A ";base64" payload is already there; anything else is
// percent-encoded bytes, which re-encode losslessly. ok is false when the
// percent-decode fails — malformed input is unrecoverable, and forwarding it
// under a base64 label would ship garbage.
func base64Payload(du dataURI) (string, bool) {
	if du.base64 {
		return du.payload, true
	}
	raw, err := url.PathUnescape(du.payload)
	if err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString([]byte(raw)), true
}

// decodePayload returns the plain-text bytes of a data: URI payload: base64 for
// a ";base64" URI, percent-decoding otherwise.
func decodePayload(du dataURI) (string, bool) {
	if du.base64 {
		decoded, err := base64.StdEncoding.DecodeString(du.payload)
		if err != nil {
			return "", false
		}
		return string(decoded), true
	}
	unescaped, err := url.PathUnescape(du.payload)
	if err != nil {
		return "", false
	}
	return unescaped, true
}

// translateTools maps OpenAI function tools onto Anthropic tools. Anthropic
// requires an input_schema and reads it as a JSON object, so a tool whose
// parameters are absent, null or any other non-object gets an empty object
// schema instead of a field the upstream rejects.
func translateTools(in []oaiTool) []antTool {
	if len(in) == 0 {
		return nil
	}
	out := make([]antTool, 0, len(in))
	for _, t := range in {
		schema := t.Function.Parameters
		if len(schema) == 0 || schema[0] != '{' {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, antTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		})
	}
	return out
}

// translateToolChoice maps the OpenAI tool_choice union onto Anthropic's. The
// second return reports whether tools may be sent at all: it is false only for
// "none", which Anthropic cannot express and which therefore drops the tools
// array along with the choice.
func translateToolChoice(raw json.RawMessage) (*antToolChoice, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	if s, ok := egress.AsJSONString(raw); ok {
		switch s {
		case "auto":
			return &antToolChoice{Type: "auto"}, true
		case "required":
			return &antToolChoice{Type: "any"}, true
		case "none":
			return nil, false
		}
		return nil, true
	}

	var tc struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &tc) == nil && tc.Function.Name != "" {
		return &antToolChoice{Type: "tool", Name: tc.Function.Name}, true
	}
	return nil, true
}

// toolInput converts OpenAI's tool-call arguments — a JSON string — into the
// JSON object Anthropic's tool_use input expects. Empty, unparseable and
// non-object arguments all become an empty object; anything else would be a
// flattenText reduces a content field (string or part array) to plain text, for
// the fields Anthropic types as text: the system prompt and tool_result content.
func flattenText(raw json.RawMessage) string {
	if s, ok := egress.AsJSONString(raw); ok {
		return s
	}
	var parts []oaiContentPart
	if json.Unmarshal(raw, &parts) == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "" || p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}
