package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// TranslateResponsesToChat converts a non-streaming Responses API response
// body into a chat.completion body. Message text, function calls and the
// reasoning summary (as reasoning_content, the field MH already normalizes
// for OpenRouter/DeepSeek) are reconstructed, the terminal status maps to
// finish_reason, and usage carries across including reasoning/cached token
// details. model is echoed to the client (the model string it requested).
func TranslateResponsesToChat(respBody []byte, model string) ([]byte, error) {
	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("openairesponses: invalid upstream response: %s", jsonfault.Describe(err, len(respBody)))
	}
	// A Responses body always carries an object id and a status; a 200 body
	// without either is not a Responses payload and must not be silently
	// translated into an empty completion.
	if resp.ID == "" && resp.Status == "" {
		return nil, fmt.Errorf("openairesponses: upstream body is not a Responses object")
	}

	msg := chatRespMessage{Role: "assistant"}
	var textParts, summaryParts []string
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" {
					textParts = append(textParts, c.Text)
				}
			}
		case "reasoning":
			for _, s := range item.Summary {
				if s.Text != "" {
					summaryParts = append(summaryParts, s.Text)
				}
			}
		case "function_call":
			args := item.Arguments
			if args == "" || !json.Valid([]byte(args)) {
				args = "{}"
			}
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			msg.ToolCalls = append(msg.ToolCalls, chatToolCall{
				ID:       id,
				Type:     "function",
				Function: chatToolCallFunc{Name: item.Name, Arguments: args},
			})
		}
	}
	if text := strings.Join(textParts, ""); text != "" {
		msg.Content = text
	}
	msg.ReasoningContent = strings.Join(summaryParts, "\n\n")

	out := chatResponse{
		ID:      chatCompletionID(resp.ID),
		Object:  "chat.completion",
		Created: resp.CreatedAt,
		Model:   resp.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: mapStatusFinishReason(resp.Status, resp.IncompleteDetails, len(msg.ToolCalls) > 0),
		}},
		Usage: translateUsage(resp.Usage),
	}
	if out.Created == 0 {
		out.Created = time.Now().Unix()
	}
	if out.Model == "" {
		out.Model = model
	}

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("openairesponses: marshal chat response: %w", err)
	}
	return body, nil
}

// chatCompletionID derives a chat-completions-style id from the upstream
// response id, synthesizing one when the upstream omitted it.
func chatCompletionID(respID string) string {
	if respID == "" {
		return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return "chatcmpl-" + strings.TrimPrefix(respID, "resp_")
}

// translateUsage maps Responses usage to the chat usage block the metering
// pipeline reads (prompt/completion totals plus reasoning and cached-token
// details).
func translateUsage(raw json.RawMessage) *chatUsage {
	// JSONMemberSet, not len(raw) > 0: a RawMessage for null is four non-empty
	// bytes where the *Usage this replaced was nil, and the Responses API emits
	// "usage": null on a non-terminal response snapshot. Reading only the length
	// turned that into a positive claim of zero tokens.
	if !util.JSONMemberSet(raw) {
		return nil
	}
	// Per FIGURE, not per block. A figure read straight off one member is right
	// or absent; a SUMMED one is only as good as its addends, and a lost addend
	// leaves a number that is wrong AND non-zero, which reads as authoritative
	// and stops estimateMissingUsage replacing it. Dropping the whole block
	// instead threw away counts that were never in doubt — and a completion
	// count is what tells the breaker the provider answered at all.
	//
	// Every figure here is read directly — the details blocks are separate
	// members, and output_tokens_details as [] rather than {} is a routine relay
	// habit that must not cost the counts beside it. Only the FALLBACK total is
	// a sum, so it is the only one an addend can take down.
	var u Usage
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return nil
	}
	lostAddend := len(util.UnreadableCounts(raw, "input_tokens", "output_tokens")) > 0
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if len(util.UnreadableCounts(raw, key)) == 0 {
			continue
		}
		switch key {
		case "input_tokens":
			u.InputTokens = 0
		case "output_tokens":
			u.OutputTokens = 0
		case "total_tokens":
			u.TotalTokens = 0
		}
	}
	out := &chatUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	// The fallback total is the one sum here, so a lost addend is the one thing
	// that can take it down.
	if out.TotalTokens == 0 && !lostAddend {
		out.TotalTokens = u.InputTokens + u.OutputTokens
	}
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
		out.PromptTokensDetails = &chatPromptTokensDetails{CachedTokens: u.InputTokensDetails.CachedTokens}
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens > 0 {
		out.CompletionTokensDetails = &chatCompletionTokensDetails{ReasoningTokens: u.OutputTokensDetails.ReasoningTokens}
	}
	return out
}
