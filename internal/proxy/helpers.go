package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
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

// normalizeFinishReasonInChoices normalizes the finish_reason value in the
// first choice of a parsed SSE chunk. It maps provider-specific values (e.g.
// "end_turn", "STOP") to OpenAI-compatible equivalents in-place, and updates
// lastReason with the final value. The model and provider params are included
// in the debug log for traceability. Replaces 3 identical inline blocks.
func normalizeFinishReasonInChoices(choices []map[string]json.RawMessage, lastReason *string, modelID, providerName string) {
	if len(choices) == 0 {
		return
	}
	frRaw, ok := choices[0]["finish_reason"]
	if !ok {
		return
	}
	var frStr string
	if json.Unmarshal(frRaw, &frStr) != nil || frStr == "" {
		return
	}
	normalized := normalizeFinishReason(frStr)
	if normalized != frStr {
		choices[0]["finish_reason"] = json.RawMessage(`"` + normalized + `"`)
		debuglog.Debug("proxy: normalized finish_reason", "original", frStr, "normalized", normalized, "model", modelID, "provider", providerName)
	}
	*lastReason = normalized
}

// extractCacheTokens returns prompt cache hit and miss token counts from a
// Usage struct. It checks three provider-specific fields in precedence order:
// PromptCacheHitTokens (OpenAI), CacheReadInputTokens (Anthropic-native),
// and PromptTokensDetails.CachedTokens (OpenAI nested format).
// Returns (0, 0) when no cache fields are present. Streaming callers should
// guard the assignment (hit > 0 || miss > 0) to avoid zeroing out cache counts
// from an earlier usage chunk; non-streaming callers can assign unconditionally
func extractCacheTokens(u Usage) (hitTokens, missTokens int) {
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens, max(0, u.PromptTokens-u.PromptCacheHitTokens)
	}
	if u.CacheReadInputTokens > 0 {
		return u.CacheReadInputTokens, max(0, u.PromptTokens-u.CacheReadInputTokens)
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens, max(0, u.PromptTokens-u.PromptTokensDetails.CachedTokens)
	}
	return 0, 0
}

// parsedChunk holds the decomposed fields from an SSE data line payload.
// Instead of nesting 5-6 levels of json.Unmarshal checks, parseChunkPayload
// returns all three maps in a single call.
type parsedChunk struct {
	raw     map[string]json.RawMessage
	choices []map[string]json.RawMessage
	delta   map[string]json.RawMessage
}

// parseChunkPayload decomposes an SSE chunk payload into its top-level map,
// choices array, and delta fields. Returns false if any step fails, allowing
// callers to replace 5-6 nested if/unmarshal blocks with a single check.
func parseChunkPayload(payload string) (parsedChunk, bool) {
	var p parsedChunk
	if json.Unmarshal([]byte(payload), &p.raw) != nil {
		return p, false
	}
	choicesRaw, ok := p.raw["choices"]
	if !ok {
		return p, false
	}
	if json.Unmarshal(choicesRaw, &p.choices) != nil || len(p.choices) == 0 {
		return p, false
	}
	deltaRaw, ok := p.choices[0]["delta"]
	if !ok {
		return p, false
	}
	if json.Unmarshal(deltaRaw, &p.delta) != nil {
		return p, false
	}
	return p, true
}

// parseAccumulatedError attempts to extract a human-readable error message
// from accumulated SSE error bytes. Some providers (e.g. OpenAI, go-openai)
// split error JSON across multiple SSE data lines. This function tries to
// parse the accumulated bytes as a complete JSON error object, supporting
// both the OpenAI format ({"error":{"message":"..."}}) and the Anthropic
// format ({"type":"error","error":{"message":"..."}}).
func parseAccumulatedError(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// Try OpenAI error format: {"error":{"message":"..."}}
	var openaiErr struct {
		Error *struct{ Message string } `json:"error"`
	}
	if json.Unmarshal(data, &openaiErr) == nil && openaiErr.Error != nil {
		return openaiErr.Error.Message
	}
	// Try Anthropic error format: {"type":"error","error":{"type":"...","message":"..."}}
	var anthErr struct {
		Type  string `json:"type"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &anthErr) == nil && anthErr.Error != nil {
		return anthErr.Error.Message
	}
	// Can't parse as structured error — return raw bytes if they start with
	// { (heuristic for truncated JSON).
	if data[0] == '{' {
		return string(data)
	}
	return ""
}

func generateRequestHash() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// recordTokenUsage adds the total token count to a virtual key's usage counter.
// On failure, it publishes a tokens.error event for the frontend toast system.
// Extracted from identical blocks in handleStreamingResponse and
// handleNonStreamingResponse.
func (h *Handler) recordTokenUsage(vkHash string, promptTokens, completionTokens, reasoningTokens int, virtualKeyName string) {
	totalTokens := promptTokens + completionTokens + reasoningTokens
	tokCtx, tokCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tokCancel()
	if err := h.virtualKeyRepo.AddTokens(tokCtx, vkHash, totalTokens); err != nil {
		keyLabel := vkHash
		if virtualKeyName != "" {
			keyLabel = virtualKeyName
		}
		events.Publish(events.Event{
			Type:     "tokens.error",
			Severity: "error",
			Source:   "proxy",
			Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
			Metadata: map[string]interface{}{"error": err.Error(), "key": keyLabel},
		})
	}
}

// writeSSEDataChunk writes an SSE "data: <payload>\n\n" sequence to w,
// updating bytesWritten with the number of bytes written. Returns an error
// if any write fails. The caller is responsible for flushing and setting
// skipNextEmptyLine/written flags. Replaces 4 identical inline write blocks.
func writeSSEDataChunk(w io.Writer, payload []byte, bytesWritten *int64) error {
	n, err := w.Write([]byte("data: "))
	*bytesWritten += int64(n)
	if err != nil {
		return err
	}
	n, err = w.Write(payload)
	*bytesWritten += int64(n)
	if err != nil {
		return err
	}
	n, err = w.Write([]byte("\n\n"))
	*bytesWritten += int64(n)
	return err
}
