package proxy

import (
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// NeedsProviderInjection returns true if the provider type requires
// parameter injection for reasoning/thinking to work correctly.
func NeedsProviderInjection(providerType string) bool {
	switch providerType {
	case "zai-coding", "opencode-zen", "opencode-go", "deepseek":
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
func InjectProviderParams(raw map[string]interface{}, providerType, modelID string) bool {
	modified := false

	switch providerType {
	case "zai-coding":
		// Z.ai / ZhipuAI requires thinking config for reasoning models.
		// Without this, reasoning_content is never returned.
		if _, exists := raw["thinking"]; !exists {
			raw["thinking"] = map[string]interface{}{
				"type":           "enabled",
				"clear_thinking": false,
			}
			modified = true
			debuglog.Debug("proxy: injected thinking config for z.ai", "model", modelID)
		}

	case "opencode-zen", "opencode-go":
		// Baseten/OpenCode Zen and Go require chat_template_args to enable thinking.
		// Without this, reasoning_content is never returned.
		if _, exists := raw["chat_template_args"]; !exists {
			raw["chat_template_args"] = map[string]interface{}{
				"enable_thinking": true,
			}
			modified = true
			debuglog.Debug("proxy: injected chat_template_args for opencode provider", "provider_type", providerType, "model", modelID)
		}

	case "deepseek":
		// DeepSeek V4 and R1 require reasoning_content on every assistant message.
		// If any assistant message lacks reasoning_content, the API rejects the request.
		modelLower := strings.ToLower(modelID)
		isReasoningModel := strings.Contains(modelLower, "v4") || strings.Contains(modelLower, "r1")
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
// array has a reasoning_content field. DeepSeek V4/R1 reject requests where
// any assistant message is missing this field.
func backfillDeepSeekReasoning(raw map[string]interface{}) bool {
	messages, ok := raw["messages"].([]interface{})
	if !ok {
		return false
	}

	modified := false
	for i, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
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
