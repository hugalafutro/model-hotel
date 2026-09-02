package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// --- Incoming OpenAI non-streaming response shape ---

type oaiResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	// Held raw and decoded on its own, so a usage block this package cannot
	// read costs the usage and nothing else. Decoded inline, one count the
	// provider spells differently (quoted, or with a fraction on it) fails the
	// whole decode and the caller gets a 502 in place of the answer.
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
	// reasoning_content, surfaced by some OpenAI-compatible providers, has no
	// field here: the translated path carries no thinking block.
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
	// ExtraContent carries the Gemini 3 thought signature on the OpenAI
	// side; raw, since a shape this package does not expect is an unsigned
	// call and not a failed translation.
	ExtraContent json.RawMessage `json:"extra_content"`
}

// readOAUsage maps an OpenAI usage block to the Anthropic token accounting.
//
// util.DecodeCounts reads a count written as "12" or 12.0; the shape tolerance
// (util.ShapeError) keeps the figure beside a member this translator has no
// field for, or one that is not a count in any spelling. Both counts are read
// straight off their own member, so an unreadable one costs only itself.
//
// Absent, null and unreadable all land on the same zeros. No JSONMemberSet
// guard: usage is mandatory here and nothing accumulates across chunks.
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
		for i, tc := range choice.Message.ToolCalls {
			input := json.RawMessage(tc.Function.Arguments)
			if len(input) == 0 || !json.Valid(input) {
				input = json.RawMessage("{}")
			}
			id := tc.ID
			if id == "" {
				// Anthropic requires a tool_use id; synthesize a stable one,
				// or a signed empty id passes the wire and comes back as an
				// empty call id.
				id = fmt.Sprintf("toolu_%s_%d", messageID, i)
			}
			msg.Content = append(msg.Content, contentBlock{
				Type:  "tool_use",
				ID:    signedToolUseID(id, egress.ThoughtSignatureIn(tc.ExtraContent)),
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
