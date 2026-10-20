// Package anthropicegress translates between the OpenAI chat-completions wire
// shape and Anthropic's native Messages shape. It is an *egress* dialect
// adapter (the mirror image of internal/anthropic, which translates on
// ingress): every chat request the proxy sends to a Messages speaker (an
// Anthropic-family provider, a Claude model on OpenCode) is rewritten here and
// sent to /v1/messages, carrying the prompt-cache breakpoints (caching.go) that
// Anthropic's OpenAI-compat /v1/chat/completions cannot.
package anthropicegress

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// DefaultMaxTokens is the max_tokens supplied when the caller sends none.
// Anthropic requires the field, and a request that omits it 400s rather than
// falling back to a model default.
const DefaultMaxTokens = 4096

// ErrInvalidRequest marks every translation failure the caller's body caused
// (as opposed to a gateway fault): a body that is not a chat request, no
// usable turn, content in a shape no part array has, or ErrUnsupportedContent.
// The proxy books such an attempt as the caller's error, a 400 and not a 502.
// Matched with errors.Is; the message is the specific reason.
var ErrInvalidRequest = errors.New("anthropicegress: invalid request")

// invalidRequestError is a caller-caused failure with its own message. Is
// reports ErrInvalidRequest, so every one matches the sentinel.
type invalidRequestError struct{ msg string }

func (e *invalidRequestError) Error() string        { return e.msg }
func (e *invalidRequestError) Is(target error) bool { return target == ErrInvalidRequest }

// invalidRequest builds an invalidRequestError with the package prefix.
func invalidRequest(format string, a ...any) error {
	return &invalidRequestError{msg: "anthropicegress: " + fmt.Sprintf(format, a...)}
}

// ErrUnsupportedContent marks a request whose message content carries a part
// the Messages API cannot be given: a file part naming a file_id (no bytes to
// send), a malformed data: URI or payload, or a part type with no Anthropic
// block (input_audio). The request is refused rather than sent without the
// part: a confident answer about a document the model never saw is worse than
// a 400. It is an ErrInvalidRequest.
var ErrUnsupportedContent = invalidRequest("content part cannot be sent to Anthropic Messages")

// minThinkingBudget is Anthropic's smallest thinking budget. An explicit
// max_tokens that cannot hold that much thinking and an answer leaves thinking
// off rather than being raised past what the caller allowed.
const minThinkingBudget = 1024

// maxTemperature is the top of Anthropic's temperature range. OpenAI's runs to
// 2, and Messages rejects a value above 1, so the translation caps it there.
const maxTemperature = 1.0

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
// equivalent (frequency_penalty, presence_penalty, n, seed, logprobs,
// stream_options, ...) are dropped by omission. response_format is the
// exception: Messages has no such field either, but dropping it would answer a
// caller who asked for JSON with prose, so it is folded into the system prompt
// (see foldResponseFormat).

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
	ParallelToolCalls   *bool           `json:"parallel_tool_calls"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	ResponseFormat      json.RawMessage `json:"response_format"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []oaiContentPart OR null
	ToolCalls  []oaiToolCall   `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type oaiContentPart struct {
	Type     string `json:"type"` // "text" | "image_url" | "file" | "refusal"
	Text     string `json:"text"`
	Refusal  string `json:"refusal"` // an assistant refusal part replayed from a transcript
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
	File *oaiFile `json:"file"`
}

type oaiFile struct {
	FileData string `json:"file_data"` // data: URI
	FileURL  string `json:"file_url"`
	// FileID names an upload on the caller's provider. Decoded only so an
	// otherwise empty file object that carries one is refused, not dropped.
	FileID string `json:"file_id"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name string `json:"name"`
		// util.ToolArguments, not a plain string: the spec says a JSON string and
		// several providers send the object, so #808 taught the RESPONSE side to
		// accept both — and an ingress decoder has no business being stricter
		// than the decoder that produced the value. This gateway rewrites the
		// object form on its own way out, so the client holding one got it from
		// another gateway or SDK; rejecting it 400s that conversation on every
		// retry, because each one replays the same transcript.
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
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []antMessage `json:"messages"`
	// System is a plain string, or []antSystemBlock when a cache breakpoint has
	// to sit on it (Anthropic accepts both; only a block can carry cache_control).
	System        any              `json:"system,omitempty"`
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

// antCacheControl marks a prompt-cache breakpoint on a tool, system block or
// content block: Anthropic caches the prefix ending there.
type antCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// antSystemBlock is one block of a system prompt in array form, the shape a
// cache breakpoint needs.
type antSystemBlock struct {
	Type         string           `json:"type"` // "text"
	Text         string           `json:"text"`
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
}

type antBlock struct {
	Type string `json:"type"` // "text" | "image" | "document" | "tool_use" | "tool_result"
	// cache_control is accepted on every block type this translator emits.
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
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
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	InputSchema  json.RawMessage  `json:"input_schema"`
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
}

type antToolChoice struct {
	Type string `json:"type"` // "auto" | "any" | "tool" | "none"
	Name string `json:"name,omitempty"`
	// DisableParallelToolUse is OpenAI's parallel_tool_calls:false.
	DisableParallelToolUse bool `json:"disable_parallel_tool_use,omitempty"`
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
// chosen by the caller, and no cache breakpoints.
func TranslateRequestWithDialect(chatBody []byte, dialect ThinkingDialect) (body []byte, model string, stream bool, err error) {
	return TranslateRequestWithOptions(chatBody, Options{Dialect: dialect})
}

// Options tunes a translation for one candidate.
type Options struct {
	// Dialect is the extended-thinking shape to ask for.
	Dialect ThinkingDialect
	// Caching places prompt-cache breakpoints (see applyCacheBreakpoints). On for
	// every request the proxy builds for a candidate (served traffic and the
	// retirement probe alike, which follows the same learned facts), off for a
	// model whose endpoint refused the field (CacheControlRefused), and off for
	// the dashboard Test button, which calls TranslateRequest. Bodies below a
	// model's minimum cacheable length are accepted and simply not cached.
	Caching bool
	// DefaultMaxTokens is the max_tokens a request that names none gets: the
	// model's own output ceiling when the caller knows it. 0 falls back to the
	// package DefaultMaxTokens.
	DefaultMaxTokens int
}

// TranslateRequestWithOptions is TranslateRequest with every per-candidate
// choice made by the caller.
func TranslateRequestWithOptions(chatBody []byte, opts Options) (body []byte, model string, stream bool, err error) {
	dialect := opts.Dialect
	var req oaiRequest
	if err := json.Unmarshal(chatBody, &req); err != nil {
		return nil, "", false, invalidRequest("invalid request body: %s", jsonfault.Describe(err, len(chatBody)))
	}
	if req.Model == "" {
		return nil, "", false, invalidRequest("model is required")
	}

	out := antRequest{
		Model:         req.Model,
		Stream:        req.Stream,
		Temperature:   capTemperature(req.Temperature),
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: nonBlankStops(egress.DecodeStop(req.Stop)),
	}

	system, messages, err := translateMessages(req.Messages)
	if err != nil {
		return nil, "", false, err
	}
	if len(messages) == 0 {
		return nil, "", false, invalidRequest("at least one user or assistant message with content is required")
	}
	// Assigned only when non-empty: an interface holding "" is not "empty" to
	// encoding/json's omitempty, and Anthropic rejects an empty system string.
	if system := foldResponseFormat(system, req.ResponseFormat); system != "" {
		out.System = system
	}
	out.Messages = messages

	out.Tools = translateTools(req.Tools)
	// Anthropic rejects a tool_choice without tools, so a choice sent alone
	// (OpenAI tolerates "none" there) is dropped with nothing to choose from.
	if len(out.Tools) > 0 {
		out.ToolChoice = translateToolChoice(req.ToolChoice)
	}
	// parallel_tool_calls:false is a flag on Anthropic's tool_choice, so a
	// request that set it without naming a choice gets the default "auto" to
	// hang it on. The "none" choice takes no flags, and forbids the calls the
	// flag would serialise anyway.
	if req.ParallelToolCalls != nil && !*req.ParallelToolCalls && len(out.Tools) > 0 {
		if out.ToolChoice == nil {
			out.ToolChoice = &antToolChoice{Type: "auto"}
		}
		if out.ToolChoice.Type != "none" {
			out.ToolChoice.DisableParallelToolUse = true
		}
	}

	// max_completion_tokens is the modern OpenAI field and wins over the
	// deprecated max_tokens when both are present. A non-positive value is no
	// budget at all — Anthropic 400s on it — so it falls through to the default:
	// the model's own output ceiling when the caller knows it (Options), else
	// DefaultMaxTokens. An explicit limit is the caller's and is never raised.
	out.MaxTokens = req.MaxCompletionTokens
	if out.MaxTokens <= 0 {
		out.MaxTokens = req.MaxTokens
	}
	explicitLimit := out.MaxTokens > 0
	if !explicitLimit {
		out.MaxTokens = opts.DefaultMaxTokens
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = DefaultMaxTokens
	}

	applyThinking(&out, req.ReasoningEffort, dialect, explicitLimit)
	if opts.Caching {
		applyCacheBreakpoints(&out)
	}

	body, err = json.Marshal(out)
	if err != nil {
		return nil, "", false, fmt.Errorf("anthropicegress: marshal messages request: %w", err)
	}
	return body, req.Model, req.Stream, nil
}

// foldResponseFormat folds an OpenAI response_format into the system prompt.
// The Messages API has no such field and no structured-output mode of its own,
// so a json_schema or json_object request that is merely dropped comes back as
// prose; the instruction the chat route folds in for a provider that will not
// enforce the schema (paramrewrite.SchemaPromptInstruction) asks for the same
// shape here. Nothing validates the answer against the schema, exactly as on
// that route. A response_format of any other shape is still dropped.
func foldResponseFormat(system string, raw json.RawMessage) string {
	var rf map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &rf) != nil {
		return system
	}
	instruction, _, ok := paramrewrite.SchemaPromptInstruction(rf)
	if !ok {
		return system
	}
	if system == "" {
		return instruction
	}
	return system + "\n\n" + instruction
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
//
// max_tokens is the other shared rule. A limit the caller set explicitly is a
// spending cap and is never raised: thinking fits under it (a budget halved to
// leave the answer room) or stays off when even minThinkingBudget would not.
// A defaulted limit is the gateway's own number and grows to make room.
func applyThinking(out *antRequest, reasoningEffort string, dialect ThinkingDialect, explicitLimit bool) {
	effort := strings.ToLower(strings.TrimSpace(reasoningEffort))

	switch dialect {
	case ThinkingBudget:
		budget, ok := thinkingBudgets[effort]
		if !ok {
			return
		}
		// The budget dialect refuses a forced tool choice alongside thinking
		// ("Thinking may not be enabled when tool_choice forces tool use",
		// live on claude-haiku-4-5 and claude-opus-4-5, 2026-09-20); the
		// adaptive dialect accepts the pair. The forced call is what the caller
		// built the request around (structured output via a named tool), so it
		// wins and thinking stays off, sampling knobs intact.
		if forcesToolUse(out.ToolChoice) {
			return
		}
		// Anthropic requires max_tokens strictly greater than the budget: the
		// remainder is the visible answer's allowance.
		if out.MaxTokens <= budget {
			if explicitLimit {
				budget = out.MaxTokens / 2
				if budget < minThinkingBudget {
					return
				}
			} else {
				out.MaxTokens = budget + DefaultMaxTokens
			}
		}
		out.Thinking = &antThinking{Type: "enabled", BudgetTokens: budget}
	default: // ThinkingAdaptive
		level, ok := thinkingEfforts[effort]
		if !ok {
			return
		}
		// The model decides how much to think, up to this ceiling, so there is no
		// budget to keep max_tokens clear of. It still needs room for thinking AND
		// an answer: a small allowance with a big effort gets a stream of pure
		// thinking cut off before any text (observed live: max_tokens 30 spent 29
		// thinking tokens and returned no content). A defaulted allowance is
		// raised; an explicit one below the smallest budget leaves thinking off.
		if explicitLimit && out.MaxTokens < minThinkingBudget {
			return
		}
		out.Thinking = &antThinking{Type: "adaptive"}
		out.OutputConfig = &antOutputConfig{Effort: level}
		if !explicitLimit && out.MaxTokens < minAdaptiveMaxTokens {
			out.MaxTokens = minAdaptiveMaxTokens
		}
	}
	out.Temperature, out.TopP, out.TopK = nil, nil, nil
}

// forcesToolUse reports whether a tool_choice makes the model call a tool:
// Anthropic's "any" and "tool" shapes.
func forcesToolUse(c *antToolChoice) bool {
	return c != nil && (c.Type == "any" || c.Type == "tool")
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

// translateToolChoice maps the OpenAI tool_choice union onto Anthropic's, which
// has a value for each: "none" is {"type":"none"}, with the tools array kept so
// the prompt prefix (and the cache breakpoint on its last tool) stays the same
// across a turn that forbids tool use.
func translateToolChoice(raw json.RawMessage) *antToolChoice {
	switch mode, name, ok := egress.DecodeToolChoice(raw); {
	case !ok:
		return nil
	case mode == "auto":
		return &antToolChoice{Type: "auto"}
	case mode == "required":
		return &antToolChoice{Type: "any"}
	case mode == "none":
		return &antToolChoice{Type: "none"}
	case mode == "function":
		return &antToolChoice{Type: "tool", Name: name}
	}
	return nil
}

// flattenText reduces a content field (string or part array) to plain text, for
// the fields Anthropic types as text: the system prompt and tool_result content.
func flattenText(raw json.RawMessage) string {
	s, _ := egress.FlattenText(raw)
	return s
}
