package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// --- Incoming Gemini generateContent response shape ---

type genResponse struct {
	Candidates     []genCandidate `json:"candidates"`
	UsageMetadata  *genUsage      `json:"usageMetadata"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

type genCandidate struct {
	Content      genRespContent `json:"content"`
	FinishReason string         `json:"finishReason"`
}

type genRespContent struct {
	Parts []genRespPart `json:"parts"`
}

type genRespPart struct {
	Text         string           `json:"text"`
	Thought      bool             `json:"thought"`
	FunctionCall *genRespFuncCall `json:"functionCall"`
}

type genRespFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type genUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
}

// --- Outgoing OpenAI chat-completion response shape ---

type oaiCompletion struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []oaiChoiceOut `json:"choices"`
	Usage   *oaiUsage      `json:"usage,omitempty"`
}

type oaiChoiceOut struct {
	Index        int           `json:"index"`
	Message      oaiMessageOut `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type oaiMessageOut struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []oaiToolCallOut `json:"tool_calls,omitempty"`
}

type oaiToolCallOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiUsage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	TotalTokens             int `json:"total_tokens"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
}

// BuildChatCompletion converts a non-streaming Gemini generateContent response
// body into an OpenAI chat-completion body. id, model and created are supplied
// by the caller (the model string the client requested is echoed back).
func BuildChatCompletion(body []byte, id, model string, created int64) ([]byte, error) {
	var resp genResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("gemini: invalid upstream response: %w", err)
	}
	if len(resp.Candidates) == 0 {
		reason := "no candidates in response"
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			reason = "prompt blocked: " + resp.PromptFeedback.BlockReason
		}
		return nil, fmt.Errorf("gemini: %s", reason)
	}

	cand := resp.Candidates[0]
	text, toolCalls := translateCandidateParts(id, cand.Content.Parts)

	msg := oaiMessageOut{Role: "assistant", Content: &text, ToolCalls: toolCalls}

	out := oaiCompletion{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []oaiChoiceOut{{
			Index:        0,
			Message:      msg,
			FinishReason: mapFinishReason(cand.FinishReason, len(toolCalls) > 0),
		}},
		Usage: translateUsage(resp.UsageMetadata),
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal chat completion: %w", err)
	}
	return encoded, nil
}

// translateCandidateParts joins visible text parts (thought parts are model
// internals and must not surface as content) and converts functionCall parts
// into OpenAI tool_calls. Gemini has no call IDs, so IDs are synthesized from
// the response id + index; TranslateRequest resolves them back by mapping,
// falling back to the function name.
func translateCandidateParts(id string, parts []genRespPart) (string, []oaiToolCallOut) {
	var sb strings.Builder
	var toolCalls []oaiToolCallOut
	for _, p := range parts {
		if p.FunctionCall != nil {
			args := compactJSON(p.FunctionCall.Args)
			if args == "" {
				args = "{}"
			}
			tc := oaiToolCallOut{
				ID:   fmt.Sprintf("call_%s_%d", id, len(toolCalls)),
				Type: "function",
			}
			tc.Function.Name = p.FunctionCall.Name
			tc.Function.Arguments = args
			toolCalls = append(toolCalls, tc)
			continue
		}
		if p.Thought {
			continue
		}
		sb.WriteString(p.Text)
	}
	return sb.String(), toolCalls
}

// compactJSON strips the pretty-print whitespace Vertex puts inside nested
// raw values (functionCall args). Invalid/empty input returns "".
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	return buf.String()
}

// mapFinishReason maps Gemini finishReason values onto OpenAI finish_reason.
func mapFinishReason(reason string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	switch reason {
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return "content_filter"
	default: // STOP, FINISH_REASON_UNSPECIFIED, MALFORMED_FUNCTION_CALL, ...
		return "stop"
	}
}

// translateUsage maps usageMetadata to OpenAI usage. Thinking tokens are
// billed output on Gemini, so completion_tokens includes them (this is what
// MH's metering should count) with the split surfaced in
// completion_tokens_details.reasoning_tokens, matching OpenAI's convention.
func translateUsage(u *genUsage) *oaiUsage {
	if u == nil {
		return nil
	}
	out := &oaiUsage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount + u.ThoughtsTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
	if u.ThoughtsTokenCount > 0 {
		out.CompletionTokensDetails = &struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		}{ReasoningTokens: u.ThoughtsTokenCount}
	}
	return out
}
