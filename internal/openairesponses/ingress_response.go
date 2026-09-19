package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// --- Responses API shapes the ingress emits ---

// responseObject is the Response the ingress writes: on a non-streaming answer
// as the body, on a stream inside response.created / response.completed. The
// members with no chat counterpart are emitted at their documented defaults so
// a strict client reads a complete object.
type responseObject struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	CreatedAt         int64              `json:"created_at"`
	Status            string             `json:"status"`
	Error             *ResponseError     `json:"error"`
	IncompleteDetails *IncompleteDetails `json:"incomplete_details"`
	Instructions      any                `json:"instructions"`
	Metadata          map[string]any     `json:"metadata"`
	Model             string             `json:"model"`
	Output            []any              `json:"output"`
	ParallelToolCalls bool               `json:"parallel_tool_calls"`
	Temperature       *float64           `json:"temperature"`
	ToolChoice        json.RawMessage    `json:"tool_choice"`
	Tools             []any              `json:"tools"`
	TopP              *float64           `json:"top_p"`
	Store             bool               `json:"store"`
	Text              responseText       `json:"text"`
	Reasoning         *Reasoning         `json:"reasoning"`
	Usage             *Usage             `json:"usage,omitempty"`
}

type responseText struct {
	Format json.RawMessage `json:"format"`
}

var textFormatPlain = json.RawMessage(`{"type":"text"}`)

// newResponseObject is a Response with every constant member set and no output
// yet. status is in_progress on a stream until the terminal event. facts, when
// given, echoes the request's own members the way OpenAI's Response does;
// tools are not echoed (the list can be large and the client sent it).
func newResponseObject(id, model, status string, createdAt int64, facts *RequestFacts) responseObject {
	out := responseObject{
		ID:                id,
		Object:            "response",
		CreatedAt:         createdAt,
		Status:            status,
		Metadata:          map[string]any{},
		Model:             model,
		Output:            []any{},
		ParallelToolCalls: true,
		ToolChoice:        json.RawMessage(`"auto"`),
		Tools:             []any{},
		Text:              responseText{Format: textFormatPlain},
	}
	if facts == nil {
		return out
	}
	if util.JSONMemberSet(facts.TextFormat) {
		out.Text.Format = facts.TextFormat
	}
	out.Reasoning = facts.Reasoning
	if facts.Instructions != "" {
		out.Instructions = facts.Instructions
	}
	out.Temperature, out.TopP = facts.Temperature, facts.TopP
	if facts.ParallelToolCalls != nil {
		out.ParallelToolCalls = *facts.ParallelToolCalls
	}
	if util.JSONMemberSet(facts.ToolChoice) {
		out.ToolChoice = facts.ToolChoice
	}
	if util.JSONMemberSet(facts.Metadata) {
		var md map[string]any
		if json.Unmarshal(facts.Metadata, &md) == nil && md != nil {
			out.Metadata = md
		}
	}
	return out
}

// outMessageItem is an assistant message output item.
type outMessageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Status  string `json:"status"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// outputTextPart is the output_text content part.
type outputTextPart struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
	Logprobs    []any  `json:"logprobs"`
}

// refusalPart is the refusal content part.
type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

// outReasoningItem carries the model's reasoning summary (reasoning_content on
// the chat side). Never encrypted content: this gateway has none to give.
type outReasoningItem struct {
	Type    string        `json:"type"`
	ID      string        `json:"id"`
	Status  string        `json:"status"`
	Summary []SummaryPart `json:"summary"`
}

// outFunctionCallItem is a function_call output item. Arguments is the
// spec's JSON string; a chat provider's object form is normalised before it
// lands here. Namespace is set on a call to a tool the request carried inside
// a namespace tool, with Name the tool's own name.
type outFunctionCallItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Arguments string `json:"arguments"`
}

func newOutputText(text string) outputTextPart {
	return outputTextPart{Type: "output_text", Text: text, Annotations: []any{}, Logprobs: []any{}}
}

func newMessageItem(id, status string, content []any) outMessageItem {
	if content == nil {
		content = []any{}
	}
	return outMessageItem{Type: "message", ID: id, Status: status, Role: "assistant", Content: content}
}

func newReasoningItem(id, status, summary string) outReasoningItem {
	return outReasoningItem{Type: "reasoning", ID: id, Status: status, Summary: []SummaryPart{{Type: "summary_text", Text: summary}}}
}

func newFunctionCallItem(id, status, callID, chatName, arguments string, facts *RequestFacts) outFunctionCallItem {
	it := outFunctionCallItem{Type: "function_call", ID: id, Status: status, CallID: callID, Name: chatName, Arguments: arguments}
	if facts != nil {
		if nt, ok := facts.ToolNames[chatName]; ok {
			it.Namespace, it.Name = nt.Namespace, nt.Name
		}
	}
	return it
}

// NewResponseID mints the id a Responses client sees for one request.
func NewResponseID() string { return "resp_" + uuidHex() }

func uuidHex() string { return strings.ReplaceAll(uuid.NewString(), "-", "") }

// normaliseArguments returns the JSON string a function_call item carries: a
// chat provider that sent no arguments, or bytes that are not JSON, is
// answered with the empty object rather than a string the client's JSON parse
// rejects.
func normaliseArguments(args util.ToolArguments) string {
	if args == "" || !json.Valid([]byte(args)) {
		return "{}"
	}
	return string(args)
}

// --- chat.completion the ingress reads ---

type chatInResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message      chatInMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	// Decoded on its own, so a count the provider spells differently costs
	// the usage and never the answer.
	Usage json.RawMessage `json:"usage"`
}

// chatInMessage is the assistant message of a chat completion, as the ingress
// reads it. reasoning_content is the field the pipeline normalises reasoning
// into; the bare "reasoning" spelling is read as a fallback.
type chatInMessage struct {
	Content          json.RawMessage `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        string          `json:"reasoning"`
	Refusal          string          `json:"refusal"`
	ToolCalls        []chatToolCall  `json:"tool_calls"`
}

func (m chatInMessage) reasoning() string {
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	return m.Reasoning
}

// BuildResponse converts a non-streaming chat.completion body into a Responses
// API Response object: the reasoning summary, the assistant message and every
// tool call become output items in that order, finish_reason maps onto the
// terminal status and usage carries across with its cache and reasoning
// details. responseID and model are what the client sees; facts echoes the
// request's members and reverses the flat chat names of namespaced tools.
func BuildResponse(chatBody []byte, responseID, model string, facts *RequestFacts) ([]byte, error) {
	var resp chatInResponse
	if err := json.Unmarshal(chatBody, &resp); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid chat completion: %s", jsonfault.Describe(err, len(chatBody)))
	}
	// A chat completion always carries a choice; a 2xx body without one is
	// not an answer and must not become an empty Response that reads as one.
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openairesponses: upstream body is not a chat completion")
	}
	choice := resp.Choices[0]
	createdAt := resp.Created
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	out := newResponseObject(responseID, model, "completed", createdAt, facts)

	if summary := choice.Message.reasoning(); summary != "" {
		out.Output = append(out.Output, newReasoningItem("rs_"+uuidHex(), "completed", summary))
	}
	var content []any
	if text, ok := egress.FlattenText(choice.Message.Content); ok && text != "" {
		content = append(content, newOutputText(text))
	}
	if choice.Message.Refusal != "" {
		content = append(content, refusalPart{Type: "refusal", Refusal: choice.Message.Refusal})
	}
	if len(content) > 0 {
		out.Output = append(out.Output, newMessageItem("msg_"+uuidHex(), "completed", content))
	}
	for _, tc := range choice.Message.ToolCalls {
		out.Output = append(out.Output, newFunctionCallItem("fc_"+uuidHex(), "completed", tc.ID, tc.Function.Name, normaliseArguments(tc.Function.Arguments), facts))
	}
	out.Status, out.IncompleteDetails = statusForFinish(choice.FinishReason)
	out.Usage = translateChatUsage(resp.Usage)

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: marshal response: %w", err)
	}
	return body, nil
}

// statusForFinish maps a chat finish_reason onto the Responses status: a
// length stop is an incomplete response that names max_output_tokens, a
// content filter one that names content_filter (the two reasons the API
// defines), everything else is a completed one.
func statusForFinish(finish string) (string, *IncompleteDetails) {
	switch finish {
	case "length":
		return "incomplete", &IncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		return "incomplete", &IncompleteDetails{Reason: "content_filter"}
	}
	return "completed", nil
}

// translateChatUsage is the inverse of translateUsage: the chat usage block
// becomes the Responses one, cached and reasoning details included. Absent,
// null and unreadable all yield nil, so the Response omits usage rather than
// claiming zero tokens.
func translateChatUsage(raw json.RawMessage) *Usage {
	if !util.JSONMemberSet(raw) {
		return nil
	}
	var u chatUsage
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return nil
	}
	lostPrompt := len(util.UnreadableCounts(raw, "prompt_tokens")) > 0
	lostCompletion := len(util.UnreadableCounts(raw, "completion_tokens")) > 0
	if lostPrompt {
		u.PromptTokens = 0
	}
	if lostCompletion {
		u.CompletionTokens = 0
	}
	if len(util.UnreadableCounts(raw, "total_tokens")) > 0 {
		u.TotalTokens = 0
	}
	out := &Usage{
		InputTokens:         u.PromptTokens,
		OutputTokens:        u.CompletionTokens,
		TotalTokens:         u.TotalTokens,
		InputTokensDetails:  &InputTokensDetails{},
		OutputTokensDetails: &OutputTokensDetails{},
	}
	// The fallback total is a sum, so a lost addend takes it down rather than
	// publishing a partial figure as the whole: the rule translateUsage reads
	// the other direction by.
	if out.TotalTokens == 0 && !lostPrompt && !lostCompletion {
		out.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	if u.PromptTokensDetails != nil {
		out.InputTokensDetails.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		out.OutputTokensDetails.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}
