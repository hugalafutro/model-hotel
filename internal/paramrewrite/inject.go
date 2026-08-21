package paramrewrite

import (
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// NeedsProviderInjection returns true if the provider type requires
// parameter injection for reasoning/thinking to work correctly.
func NeedsProviderInjection(providerType string) bool {
	switch providerType {
	case "zai-coding", "opencode-go", "deepseek":
		return true
	}
	return false
}

// InjectProviderParams modifies the raw request body map to inject
// provider-specific parameters required for reasoning/thinking to work.
// Returns true if any modifications were made.
//
// This is necessary because model-hotel acts as a transparent proxy;
// clients like opencode don't know which upstream provider they're
// really talking to, so they can't send provider-specific options.
func InjectProviderParams(raw map[string]any, providerType, modelID string) bool {
	modified := false

	switch providerType {
	case "zai-coding":
		// Z.ai / ZhipuAI requires thinking config for reasoning models.
		// Without this, reasoning_content is never returned.
		if _, exists := raw["thinking"]; !exists {
			raw["thinking"] = map[string]any{
				"type":           "enabled",
				"clear_thinking": false,
			}
			modified = true
			debuglog.Debug("proxy: injected thinking config for z.ai", "model", modelID)
		}

	// NOTE: opencode-zen is deliberately absent. It used to share this branch,
	// but Zen's backend no longer tolerates chat_template_args: live A/B testing
	// on 2026-07-30 showed it breaks 6 of 7 models with "Upstream request
	// failed" (glm-5, glm-5.1, glm-5.2, minimax-m3, kimi-k2.6, kimi-k3,
	// deepseek-v4-pro all fail with it and succeed without it; only qwen3.6-plus
	// tolerates it). Zen's own error is generic enough that the 400 param-strip
	// retry does not recognise it as a parameter problem, so those models were
	// simply unusable through the gateway. OpenCode Go still accepts the
	// parameter (verified on the same day against glm-5.2, hy3, mimo-v2.5 and
	// qwen3.7-max), so Go keeps it and only Zen loses it.
	case "opencode-go":
		// Baseten/OpenCode Go requires chat_template_args to enable thinking.
		// Without this, reasoning_content is never returned.
		if _, exists := raw["chat_template_args"]; !exists {
			raw["chat_template_args"] = map[string]any{
				"enable_thinking": true,
			}
			modified = true
			debuglog.Debug("proxy: injected chat_template_args for opencode provider", "provider_type", providerType, "model", modelID)
		}

	case "deepseek":
		// Backfills reasoning_content onto assistant messages. DeepSeek used to
		// reject a request when any assistant message lacked the field, which is
		// why this exists.
		//
		// That requirement no longer holds: on 2026-08-21 every current model
		// (deepseek-v4-flash, -pro, -flash-vision-exp and the aliases) accepted
		// assistant turns with no reasoning_content, including the shapes most
		// likely to trip it — a turn carrying tool_calls, and two assistant
		// turns in a row. The backfill is kept as insurance rather than removed
		// on the strength of one afternoon's probing, since re-adding it after a
		// silent upstream change would mean debugging it from a 400 first.
		//
		// deepseek-reasoner is matched by name because it names no version: it
		// is a permanent alias onto deepseek-v4-flash with thinking on, so it
		// reaches the same backend as an id the substring test already covers.
		// deepseek-chat stays out on purpose — it is the same model with
		// thinking off, and returns no reasoning_content to echo back.
		modelLower := strings.ToLower(modelID)
		isReasoningModel := strings.Contains(modelLower, "v4") ||
			strings.Contains(modelLower, "r1") ||
			modelLower == "deepseek-reasoner"
		if isReasoningModel {
			if backfillDeepSeekReasoning(raw) {
				modified = true
				debuglog.Debug("proxy: backfilled reasoning_content on assistant messages for deepseek", "model", modelID)
			}
		}
	}

	return modified
}

// backfillDeepSeekReasoning ensures every assistant message in the messages
// array has a reasoning_content field. See the "deepseek" case above for why
// this is kept now that the API accepts messages without it.
func backfillDeepSeekReasoning(raw map[string]any) bool {
	messages, ok := raw["messages"].([]any)
	if !ok {
		return false
	}

	modified := false
	for i, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role != "assistant" {
			continue
		}
		// Only backfill if reasoning_content is absent.
		// If it's present (even as empty string), leave it alone.
		if _, exists := msgMap["reasoning_content"]; !exists {
			msgMap["reasoning_content"] = ""
			messages[i] = msgMap
			modified = true
		}
	}

	return modified
}
