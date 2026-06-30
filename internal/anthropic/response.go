package anthropic

import (
	"encoding/json"
	"fmt"
)

// --- Incoming OpenAI non-streaming response shape ---

type oaiResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	Usage   *OAUsage    `json:"usage"`
}

type oaiChoice struct {
	Message      oaiRespMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type oaiRespMessage struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []oaiRespToolCall `json:"tool_calls"`
	// reasoning_content is surfaced by some OpenAI-compatible providers; v1
	// drops it on the translated path (thinking-block mapping is deferred).
}

type oaiRespToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// BuildMessageResponse converts a non-streaming OpenAI chat-completion response
// body into an Anthropic Messages response body. model is echoed back to the
// client (the model string it requested); messageID is the Anthropic id to
// surface. It reconstructs text and tool_use content blocks, maps the stop
// reason, and carries token usage across.
func BuildMessageResponse(body []byte, messageID, model string) ([]byte, error) {
	var resp oaiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: invalid upstream response: %w", err)
	}

	msg := message{
		ID:      messageID,
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []contentBlock{},
		Usage:   usage{InputTokens: 0, OutputTokens: 0},
	}

	finish := "stop"
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		finish = choice.FinishReason

		if choice.Message.Content != "" {
			msg.Content = append(msg.Content, contentBlock{
				Type: "text",
				Text: choice.Message.Content,
			})
		}
		for _, tc := range choice.Message.ToolCalls {
			input := json.RawMessage(tc.Function.Arguments)
			if len(input) == 0 || !json.Valid(input) {
				input = json.RawMessage("{}")
			}
			msg.Content = append(msg.Content, contentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}

	stop := mapStopReason(finish)
	msg.StopReason = &stop

	if resp.Usage != nil {
		msg.Usage.InputTokens = resp.Usage.PromptTokens
		msg.Usage.OutputTokens = resp.Usage.CompletionTokens
	}

	out, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal message response: %w", err)
	}
	return out, nil
}
