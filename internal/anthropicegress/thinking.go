package anthropicegress

import (
	"encoding/json"
	"strings"
)

// The thinking-dialect self-heal. A model accepts the adaptive thinking shape,
// the older budget shape, or both, and nothing in the model id says which (see
// ThinkingDialect). Rather than guess from a name that a third-party Messages
// endpoint need not follow at all, the proxy sends its current best guess and
// reads the answer: Anthropic names the shape it wanted in the 400 body, in
// terms specific enough to act on.
//
// The two messages, live 2026-08-20:
//
//	"thinking.type.enabled" is not supported for this model. Use
//	"thinking.type.adaptive" and "output_config.effort" to control thinking behavior.
//
//	adaptive thinking is not supported on this model
//
// Each names the dialect it rejected AND, in the first case, the one to use
// instead. Matching is on the distinguishing phrases rather than the whole
// string, so a reworded message keeps working as long as it still says which
// shape is unsupported.

// anthropicErrorEnvelope is the error body shape both messages arrive in.
type anthropicErrorEnvelope struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// DialectFromError reports the thinking dialect an upstream 400 is asking for.
// ok is false for any body that is not one of these two complaints, which is
// almost all of them: a 400 about a document, a bad model id or a malformed
// message must not be mistaken for a dialect switch and retried unchanged.
//
// The check is deliberately narrow in one more way. A body has to mention
// thinking at all before either phrase is consulted, so an unrelated error that
// happens to contain the word "adaptive" cannot flip a model onto a shape it
// does not support and cost every later request a wasted round-trip.
func DialectFromError(body []byte) (dialect ThinkingDialect, ok bool) {
	var env anthropicErrorEnvelope
	if json.Unmarshal(body, &env) != nil {
		return 0, false
	}
	msg := strings.ToLower(env.Error.Message)
	if !strings.Contains(msg, "thinking") {
		return 0, false
	}
	switch {
	// "thinking.type.enabled is not supported for this model" — the budget shape
	// was refused, so this model is adaptive-only.
	case strings.Contains(msg, "thinking.type.enabled") && strings.Contains(msg, "not supported"):
		return ThinkingAdaptive, true
	// "adaptive thinking is not supported on this model" — the reverse.
	case strings.Contains(msg, "adaptive") && strings.Contains(msg, "not supported"):
		return ThinkingBudget, true
	}
	return 0, false
}

// RequestAsksForThinking reports whether a translated Messages body carries a
// thinking request. It is what makes the retry conditional: a 400 naming a
// dialect is only worth re-issuing if the request that earned it actually asked
// for thinking, and re-issuing one that did not would repeat the same body and
// the same 400.
func RequestAsksForThinking(messagesBody []byte) bool {
	var probe struct {
		Thinking json.RawMessage `json:"thinking"`
	}
	if json.Unmarshal(messagesBody, &probe) != nil {
		return false
	}
	return len(probe.Thinking) > 0 && string(probe.Thinking) != "null"
}
