package proxy

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

func extractStreamingUsage(data string) *Usage {
	scanner := bufio.NewScanner(strings.NewReader(data))
	var lastUsage *Usage
	for scanner.Scan() {
		line := scanner.Text()
		var payload string
		//nolint:gocritic // if-else chain is clearer than switch for SSE prefix matching
		if strings.HasPrefix(line, "data: ") {
			payload = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") && len(line) > 5 {
			// "data:" with no space — LM Studio compatibility.
			payload = strings.TrimLeft(line[5:], " \t")
		} else {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Usage *Usage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) == nil && chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}
	return lastUsage
}

// normalizeFinishReason maps provider-specific finish reasons to
// OpenAI-compatible values. Different providers use different vocabularies:
//
//	Anthropic:  end_turn, max_tokens, stop_sequence, tool_use, refusal
//	Gemini:     STOP, MAX_TOKENS, SAFETY, RECITATION, OTHER, BLOCKED
//	Cohere:     COMPLETE, MAX_TOKENS, STOP_SEQUENCE, ERROR, ERROR_TOXIC
//	DeepSeek:   stop, length, content_filter, tool_calls, insufficient_system_resource
//	xAI:        stop, length, content_filter, tool_calls, insufficient_system_resource
//
// The proxy forwards SSE lines transparently, but when we parse a data line
// for usage/error extraction we also normalize finish_reason so that the
// downstream client sees consistent values.
var finishReasonMap = map[string]string{
	// Anthropic
	"end_turn":      "stop",
	"stop_sequence": "stop",
	"max_tokens":    "length",
	"tool_use":      "tool_calls",
	"refusal":       "content_filter",

	// Gemini / Vertex AI
	"STOP":       "stop",
	"MAX_TOKENS": "length",
	"SAFETY":     "content_filter",
	"RECITATION": "content_filter",
	"BLOCKED":    "content_filter",

	// Cohere
	"COMPLETE":    "stop",
	"ERROR_TOXIC": "content_filter",

	// DeepSeek / xAI
	"insufficient_system_resource": "length",

	// HuggingFace / Together AI
	"eos_token": "stop",
	"eos":       "stop",

	// Bedrock
	"guardrail_intervened": "content_filter",

	// Generic fallbacks
	"FINISH_REASON_UNSPECIFIED": "stop",
}

// normalizeFinishReason returns the OpenAI-compatible finish_reason for the
// given value, or the original value if no mapping exists. This ensures
// downstream clients see consistent finish reasons regardless of provider.
func normalizeFinishReason(reason string) string {
	if mapped, ok := finishReasonMap[reason]; ok {
		return mapped
	}
	return reason
}

func generateRequestHash() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
