package openairesponses

import (
	"encoding/json"
	"strings"
)

// RequiresResponsesAPI reports whether an upstream 400 error body is the
// OpenAI "use /v1/responses" rejection: newest models (gpt-5.4+, gpt-5.6)
// refuse function tools combined with reasoning over chat-completions and
// name the Responses API as the fix. Detection is deliberately conservative —
// the message must mention responses AND reasoning AND tools — so ordinary
// param-rejection 400s keep flowing to the param-strip self-heal.
func RequiresResponsesAPI(errBody []byte) bool {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(errBody, &envelope) != nil || envelope.Error.Message == "" {
		return false
	}
	m := strings.ToLower(envelope.Error.Message)
	return strings.Contains(m, "responses") &&
		strings.Contains(m, "reasoning") &&
		strings.Contains(m, "tool")
}

// NeedsResponsesRouting reports whether a chat-completions request body
// carries the combination that forces the Responses API on a flagged model:
// tools present AND reasoning not explicitly disabled. Absent reasoning_effort
// counts — these models reason by default, so tools-only requests without an
// explicit "none" hit the same upstream 400. Tools-free or reasoning-off
// requests keep the cheaper chat-completions path (plan §4.1).
func NeedsResponsesRouting(chatBody []byte) bool {
	var probe struct {
		Tools           []json.RawMessage `json:"tools"`
		ReasoningEffort string            `json:"reasoning_effort"`
	}
	if json.Unmarshal(chatBody, &probe) != nil {
		return false
	}
	return len(probe.Tools) > 0 && probe.ReasoningEffort != "none"
}
