package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// --- Incoming OpenAI non-streaming response shape ---

type oaiResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	// Held raw and decoded on its own, so a usage block this package cannot read
	// costs the usage and nothing else. Decoded inline it was part of the
	// response object, and one count the provider spelled differently — quoted,
	// or with a fraction on it — failed the whole decode, which the caller met
	// as a 502 in place of the answer the model had already produced. The
	// streaming twin of this path already reads counts that way; see
	// proxy.anthropicResponseWriter.handleStreamLine.
	Usage json.RawMessage `json:"usage"`
}

type oaiChoice struct {
	Message      oaiRespMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type oaiRespMessage struct {
	Role string `json:"role"`
	// Content is normally a JSON string, but some OpenAI-compatible providers
	// emit an array of content parts. Decoded via decodeRespContent so a
	// structured-array response does not fail the whole translation.
	Content   json.RawMessage   `json:"content"`
	ToolCalls []oaiRespToolCall `json:"tool_calls"`
	// reasoning_content is surfaced by some OpenAI-compatible providers; v1
	// drops it on the translated path (thinking-block mapping is deferred).
}

// decodeRespContent extracts assistant text from an OpenAI chat-completion
// message "content": a JSON string (the norm), an array of {type,text} content
// parts (some providers), or null/absent (-> ""). Non-text parts are ignored.
func decodeRespContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "" || p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

type oaiRespToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string             `json:"name"`
		Arguments util.ToolArguments `json:"arguments"`
	} `json:"function"`
}

// readOAUsage maps an OpenAI usage block to the Anthropic token accounting.
//
// util.DecodeCounts, so a count written as "12" or 12.0 is still a count; the
// shape tolerance (util.ShapeError) so a member this translator has no field
// for, or one figure that is not a count in any spelling, keeps the figure
// beside it. Both counts are read straight off their own member — neither is a
// sum — so an unreadable one costs only itself.
//
// Absent, null and unreadable all land on the same zeros, and there is no
// util.JSONMemberSet guard here because there is nothing for it to protect: the
// Anthropic Message schema makes usage mandatory, this translation reports one
// response rather than accumulating across chunks, and metering reads the
// upstream body, not this output.
func readOAUsage(raw json.RawMessage) usage {
	var u OAUsage
	if err := util.DecodeCounts(raw, &u); err != nil && util.ShapeError(raw, err) == nil {
		return usage{}
	}
	return usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
}

// BuildMessageResponse converts a non-streaming OpenAI chat-completion response
// body into an Anthropic Messages response body. model is echoed back to the
// client (the model string it requested); messageID is the Anthropic id to
// surface. It reconstructs text and tool_use content blocks, maps the stop
// reason, and carries token usage across.
func BuildMessageResponse(body []byte, messageID, model string) ([]byte, error) {
	var resp oaiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: invalid upstream response: %s", jsonfault.Describe(err, len(body)))
	}

	msg := message{
		ID:      messageID,
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []contentBlock{},
	}

	finish := "stop"
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		finish = choice.FinishReason

		if text := decodeRespContent(choice.Message.Content); text != "" {
			msg.Content = append(msg.Content, contentBlock{
				Type: "text",
				Text: text,
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

	msg.Usage = readOAUsage(resp.Usage)

	out, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal message response: %w", err)
	}
	return out, nil
}
