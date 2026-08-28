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
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			payload = after
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

// recordTokenUsage charges a completed request's token total against every
// budget that applies to it: the TPM limiter's minute budget and, when the
// request came in on a virtual key, that key's usage counter. On a counter
// failure it publishes a tokens.error event for the frontend toast system.
//
// This is the completion half of the TPM limiter's
// admit-on-past-consumption / debit-on-completion scheme, so which bucket gets
// charged must match which bucket admission reserved from:
//
//   - a keyed request (/v1) debits by key hash, and Debit reaches the owner's
//     aggregate bucket itself through the association recorded at admission;
//   - a keyless request (admin chat, session-authenticated) has no key bucket
//     and debits the owner's bucket directly.
//
// The two are exclusive on purpose: running both would charge an owner twice.
func (h *Handler) recordTokenUsage(vkHash string, logData *requestLogData, promptTokens, completionTokens, reasoningTokens int) {
	totalTokens := promptTokens + completionTokens + reasoningTokens
	if h.tpmLimiter != nil {
		switch {
		case vkHash != "":
			h.tpmLimiter.Debit(vkHash, totalTokens)
		default:
			h.tpmLimiter.DebitUser(logData.ownerUserID, totalTokens)
		}
	}
	if vkHash == "" {
		// No key row to meter: the virtual_keys usage counter is keyed by hash,
		// and admin chat has no key. The TPM debit above is the whole job here.
		return
	}
	tokCtx, tokCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tokCancel()
	if err := h.virtualKeyRepo.AddTokens(tokCtx, vkHash, totalTokens); err != nil {
		keyLabel := vkHash
		if logData.virtualKeyName != "" {
			keyLabel = logData.virtualKeyName
		}
		events.Publish(events.Event{
			Type:     "tokens.error",
			Severity: "error",
			Source:   "proxy",
			Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
			Metadata: map[string]any{"error": err.Error(), "key": keyLabel},
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

// breakerAction represents the circuit-breaker recording decision for a
// given upstream HTTP status code.
type breakerAction int

const (
	// breakerActionFailure records a failure (provider is unhealthy).
	breakerActionFailure breakerAction = iota
	// breakerActionNoOp does nothing (model-specific client error; provider is
	// alive but rejecting this request). Neither failure nor success — recording
	// success would erase real 5xx history and prematurely close half-open circuits.
	breakerActionNoOp
	// breakerActionSuccess records a success (provider is healthy).
	breakerActionSuccess
)

// breakerRecordAction determines the circuit-breaker recording action for a
// given upstream HTTP status code. This is the single source of truth for the
// status→breaker mapping and is intended to be table-tested.
//
// Note on 429: this function maps it to a failure, but it is only consulted in
// the failover-eligible branch of ChatCompletions — i.e. when shouldFailover
// already returned true, which for 429 means failover_on_rate_limit is ON. When
// that setting is OFF, a 429 is not failover-eligible and never reaches this
// function; the caller's else branch intentionally records it as a success
// (stay on the rate-limited provider rather than tripping its breaker). The 429
// treatment is therefore consistent with the configured policy, not contradictory.
func breakerRecordAction(statusCode int) breakerAction {
	switch {
	case statusCode >= 500 || statusCode == 429 || statusCode == 401 || statusCode == 403 || statusCode == 402:
		// 5xx = server error (provider unhealthy)
		// 429 = rate limit (provider overloaded; see policy note above)
		// 401/403 = auth failure (provider-wide bad/expired key)
		// 402 = out of credit (provider-wide billing condition, same class)
		return breakerActionFailure
	case statusCode == 404 || statusCode == 499:
		// 404 = stale/renamed model (model-specific, not provider health)
		// 499 = client closed request (Nginx convention; not a provider signal)
		return breakerActionNoOp
	default:
		// Any other response (200, 400, etc.) indicates the provider is alive.
		return breakerActionSuccess
	}
}

// estimateTokens converts a byte length of text into a token estimate at the
// conventional 4 bytes per token, rounding up so any delivered text charges at
// least one token. The ratio is tuned for Latin text; multi-byte scripts run
// closer to one token per character and estimate low.
func estimateTokens(textBytes int) int {
	return (textBytes + bytesPerToken - 1) / bytesPerToken
}

// bytesPerToken is the conventional text-to-token ratio the estimates above use.
const bytesPerToken = 4

// minPassthroughTokens is the floor chargePassthroughUsage applies to a
// pass-through request that was delivered but whose prompt sizes to nothing. It exists because the multipart
// families (speech-to-text, image edits and variations) carry their payload as
// an uploaded file, which is deliberately not measured, and their only promptable
// field is optional or absent — so the honest estimate is zero and the request
// would otherwise be free.
//
// One token, not a guess at the upload's real cost. Anything proportional to
// file bytes would invent a charge: audio is billed by duration and images by
// dimension, neither of which a byte count approximates. The floor's job is to
// stop a served request metering as nothing, so it reaches tokens_used and the
// TPM bucket at all; a provider that reports real usage always displaces it.
//
// It does NOT reach the request log, which continues to report only what the
// provider measured, so a metered-by-floor request still shows 0 tokens there.
// That is the same split the chat and streaming estimates keep.
const minPassthroughTokens = 1

// estimateMissingUsage fills in token counts the provider did not report, so a
// request that delivered output is always charged against the TPM budget and
// the key's tokens_used counter. Usage arrives in the LAST chunk of an OpenAI
// stream, so a client that disconnects after the content but before it (the
// upstream request is cancelled with the client, the chunk never arrives) or a
// provider that omits usage altogether would otherwise meter as zero while the
// provider bills the operator for the real tokens.
//
// Each side is estimated independently and only when it is missing: a native
// Anthropic stream reports input_tokens up front and output_tokens at the end,
// so a truncated one keeps its reported prompt and estimates only the output.
// Nothing is estimated when no output was delivered (an error before the first
// token costs nothing), and the request log keeps the provider's figures:
// estimates charge the quota, they are not reported as measured usage.
func estimateMissingUsage(promptTokens, completionTokens, reasoningTokens int, logData *requestLogData, deliveredBytes int) (prompt, completion, reasoning int) {
	outputTokens := completionTokens + reasoningTokens
	if deliveredBytes == 0 || (promptTokens > 0 && outputTokens > 0) {
		return promptTokens, completionTokens, reasoningTokens
	}
	promptEstimated, completionEstimated := promptTokens == 0, outputTokens == 0
	if promptEstimated {
		promptTokens = estimateTokens(logData.promptTextBytes)
	}
	if completionEstimated {
		completionTokens = estimateTokens(deliveredBytes)
	}
	debuglog.Info("proxy: charging estimated tokens for usage the provider did not report", "model", logData.modelID, "provider", logData.providerName, "prompt_estimated", promptEstimated, "completion_estimated", completionEstimated, "prompt_text_bytes", logData.promptTextBytes, "delivered_bytes", deliveredBytes, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "reasoning_tokens", reasoningTokens)
	return promptTokens, completionTokens, reasoningTokens
}

// passthroughPromptTextBytes sizes the prompt text of a multimodal pass-through
// request body, the way promptTextBytes does for a chat body. Those endpoints
// carry no "messages", so promptTextBytes sizes every one of them as zero and
// any estimate derived from it is silently no charge at all.
//
// Only text is counted, never a file or a blob: an audio upload or a base64
// image is orders of magnitude larger than the tokens it costs, and sizing it
// by bytes would invent a colossal charge. That is the same rule promptTextBytes
// applies when it skips image_url and input_audio parts.
//
// Embeddings input may also arrive pre-tokenised as arrays of token ids. Those
// are a token count already, so they are converted back to the byte scale the
// caller divides by (bytesPerToken) rather than measured as JSON text, which
// would charge for the digits.
func passthroughPromptTextBytes(body []byte, endpointType string) int {
	switch endpointType {
	case endpointTypeEmbeddings:
		return embeddingsInputBytes(body)
	case endpointTypeRerank:
		var req struct {
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
		}
		if json.Unmarshal(body, &req) != nil {
			return 0
		}
		n := len(req.Query)
		for _, d := range req.Documents {
			n += len(d)
		}
		return n
	case endpointTypeImage, endpointTypeTTS:
		// Image generation prompts and text-to-speech input are both a single
		// text field; TTS uses "input", images use "prompt".
		var req struct {
			Prompt string `json:"prompt"`
			Input  string `json:"input"`
		}
		if json.Unmarshal(body, &req) != nil {
			return 0
		}
		return len(req.Prompt) + len(req.Input)
	default:
		// Multipart families (speech-to-text, image edits) carry their payload
		// as an uploaded file, which has no honest text size, so they are left
		// unsized rather than guessed at.
		return 0
	}
}

// embeddingsInputBytes sizes an embeddings "input", which the OpenAI schema
// allows as a string, an array of strings, an array of token ids, or an array of
// token-id arrays.
func embeddingsInputBytes(body []byte) int {
	var req struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Input) == 0 {
		return 0
	}
	var text string
	if json.Unmarshal(req.Input, &text) == nil {
		return len(text)
	}
	var texts []string
	if json.Unmarshal(req.Input, &texts) == nil {
		n := 0
		for _, t := range texts {
			n += len(t)
		}
		return n
	}
	// Pre-tokenised: a flat token array, or one array per input.
	var tokens []int
	if json.Unmarshal(req.Input, &tokens) == nil {
		return len(tokens) * bytesPerToken
	}
	var batches [][]int
	if json.Unmarshal(req.Input, &batches) == nil {
		n := 0
		for _, b := range batches {
			n += len(b)
		}
		return n * bytesPerToken
	}
	return 0
}

// promptTextBytes sizes the prompt text of an OpenAI-shaped request body: the
// message text (string content and text parts) plus the tool definitions. It
// deliberately skips image_url and input_audio parts, which are base64 blobs
// roughly a thousand times larger than the handful of tokens they cost; sizing
// those by bytes would turn one vision request into a phantom multi-million
// token charge. A body that does not parse sizes as zero.
func promptTextBytes(body []byte) int {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &req) != nil {
		return 0
	}
	n := len(req.Tools)
	for _, m := range req.Messages {
		// Deliberately NOT deltaText, despite the shapes looking alike. The two
		// answer different questions and must not be merged: this one SKIPS a
		// part it does not recognise, because a vision request's base64 media
		// must not be sized, while deltaText has to report that it failed to
		// understand a part — the reasoning-strip contract depends on knowing
		// when the gateway cannot see inside a delta. Sharing them broke this.
		var text string
		if json.Unmarshal(m.Content, &text) == nil {
			n += len(text)
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(m.Content, &parts) == nil {
			for _, p := range parts {
				n += len(p.Text)
			}
		}
	}
	return n
}

// chatAnswerBytes sizes the output of a non-streaming answer (content,
// reasoning in any of the provider spellings, and tool calls), the delivered
// output estimateMissingUsage works from.
func chatAnswerBytes(out ChatCompletionResponse) int {
	n := 0
	for _, choice := range out.Choices {
		msg := choice.Message
		switch c := msg.Content.(type) {
		case string:
			n += len(c)
		case []any:
			for _, part := range c {
				if m, ok := part.(map[string]any); ok {
					if text, ok := m["text"].(string); ok {
						n += len(text)
					}
				}
			}
		}
		n += len(msg.ReasoningContent) + len(msg.Reasoning)
		for _, rd := range msg.ReasoningDetails {
			n += len(rd.Text)
		}
		for _, tc := range msg.ToolCalls {
			n += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	return n
}
