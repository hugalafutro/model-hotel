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

// IsResponsesOnlyRejection reports the chat-completions refusal OpenAI
// answers for a model that is served by the Responses API alone (the pro
// tier: o1-pro, o3-pro, gpt-5-pro and its point releases). The message
// misdirects, pointing at the legacy /v1/completions, and arrives as a 404
// rather than a 400; unlike the tools+reasoning rejection it applies to
// every request for the model, tools or not.
func IsResponsesOnlyRejection(errBody []byte) bool {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(errBody, &envelope) != nil || envelope.Error.Message == "" {
		return false
	}
	m := strings.ToLower(envelope.Error.Message)
	return strings.Contains(m, "not a chat model") && strings.Contains(m, "chat/completions")
}

// ResponsesOnlyModel reports an OpenAI model id known to be served by the
// Responses API alone, so the first request routes there rather than paying
// a 404 to learn it: the pro tier, by name.
func ResponsesOnlyModel(modelID string) bool {
	id := strings.ToLower(modelID)
	if strings.HasPrefix(id, "o1-pro") || strings.HasPrefix(id, "o3-pro") {
		return true
	}
	if !strings.HasPrefix(id, "gpt-5") {
		return false
	}
	rest := strings.TrimPrefix(id, "gpt-5")
	// gpt-5-pro, gpt-5.5-pro, gpt-5.5-pro-2026-04-23; not gpt-5-mini or a
	// hypothetical gpt-5-prose.
	for len(rest) > 0 && (rest[0] == '.' || rest[0] >= '0' && rest[0] <= '9') {
		rest = rest[1:]
	}
	return rest == "-pro" || strings.HasPrefix(rest, "-pro-")
}
