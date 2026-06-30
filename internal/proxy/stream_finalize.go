package proxy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Progressive stall timeout knobs, shared by the scanner loop (which pings the
// watchdog) and finalizeStream (which reconstructs the effective stall window
// for diagnostics). Lifted to package scope from handleStreamingResponse in
// Phase 2 of the streaming-pipeline refactor so both can reference one source.
const (
	progressiveChunkThreshold  = 50
	progressiveStallMultiplier = 3
)

// streamState accumulates the per-stream metrics and carry flags that the
// scanner loop in handleStreamingResponse produces and finalizeStream consumes.
// Introduced in Phase 2 of the streaming-pipeline refactor as the explicit
// hand-off between the loop and the finalizer; later phases migrate more of the
// loop-local accumulators here.
type streamState struct {
	promptTokens          int
	completionTokens      int
	reasoningTokens       int
	promptCacheHitTokens  int
	promptCacheMissTokens int
	chunkCount            int
	errorChunkCount       int
	lastErrMsg            string
	sawDone               bool
	sawMessageStop        bool // native Anthropic passthrough: terminal message_stop event seen
	clientDisconnected    bool
	stalled               bool

	// Observer state carried across chunks (Phase 4). Not consumed by the
	// finalizer, but co-located here so the data-chunk observers operate on one
	// named accumulator instead of a fistful of loop-locals.
	lastNativeFinishReason string // P2-7
	sawThinking            bool   // first-occurrence reasoning log
	lastContent            string // P2-5 repeated-content detection
	repeatedCount          int    // P2-5
	errAccum               []byte // P1-B split-error accumulation
	lastFinishReason       string // P2-2 duplicate-finish suppression + normalization carry
	lastAnthropicEvent     string // P1-C: last "event:" type, consumed by the next data line
}

// finalizeStream performs the end-of-stream bookkeeping that used to live under
// the handleStreamingResponse `logUpdate:` label: TPS computation, scanner-error
// and stall classification, missing-[DONE] injection vs "truncated", the final
// updateRequestLog, circuit-breaker failure on stall, and token-usage recording.
//
// Extracted in Phase 2 of the streaming-pipeline refactor; behavior is
// unchanged. The watchdog teardown stays in the orchestrator (it owns the
// watchdog goroutine), so st.stalled is read from the atomic before this runs.
// statusCode is the upstream response status (resp.StatusCode); scanErr is the
// reader's terminal scanner error (reader.err()).
func (h *Handler) finalizeStream(st *streamState, sink *streamSink, scanErr error, logData *requestLogData, opts streamOptions, statusCode int, startTime time.Time) {
	totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
	var tps float64
	// Use total output tokens (text + reasoning) for TPS numerator,
	// and generation time as denominator. Prefer true TTFT (first token)
	// when the probe measured it; fall back to response header time.
	totalOutputTokens := st.completionTokens + st.reasoningTokens
	ttftForTPS := opts.responseHeaderMs
	if opts.trueTtftMs > 0 {
		ttftForTPS = opts.trueTtftMs
	}
	generationDuration := totalDuration - ttftForTPS
	// Avoid absurd TPS when generation time is negligible
	// (e.g. non-streaming where response_header_ms ≈ duration_ms).
	minGeneration := max(1.0, totalDuration*0.05)
	if totalOutputTokens > 0 && generationDuration >= minGeneration {
		tps = float64(totalOutputTokens) / float64(generationDuration) * 1000
	} else if totalOutputTokens > 0 && totalDuration > 0 {
		tps = float64(totalOutputTokens) / float64(totalDuration) * 1000
	}

	errMsg := deriveStreamError(st, scanErr, opts, logData)
	if errMsg == "" && !st.sawDone && opts.rawPassthrough {
		// Native Anthropic passthrough: the Messages stream ends with a
		// message_stop event + EOF and never sends a [DONE] sentinel. A clean EOF
		// *with* message_stop is a real completion; a clean EOF *without* it means
		// the upstream dropped mid-stream, which must log as truncated (and must
		// NOT bill the partial output as a complete response). We never inject
		// [DONE] here — Anthropic clients don't expect it.
		if st.sawMessageStop {
			debuglog.Debug("proxy: native anthropic stream completed (message_stop seen)", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
		} else {
			errMsg = "stream truncated: upstream closed before message_stop"
			logData.errorKind = KindProviderError
			debuglog.Warn("proxy: native anthropic stream ended without message_stop", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
		}
	} else if errMsg == "" && !st.sawDone {
		// Upstream closed without [DONE] sentinel. If we received content and
		// the scanner didn't error, inject the sentinel for the downstream
		// client so the frontend knows the stream completed normally.
		if !st.clientDisconnected && scanErr == nil && st.chunkCount > 0 {
			debuglog.Info("proxy: upstream omitted [DONE] sentinel; injecting for downstream", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
			if err := sink.write([]byte("data: [DONE]\n\n")); err != nil {
				debuglog.Warn("proxy: failed to write injected [DONE]", "model", logData.modelID, "provider", logData.providerName, "error", err)
			} else {
				sink.flush()
			}
			// Stream was complete; the missing sentinel is benign.
			debuglog.Info("proxy: stream completed (upstream omitted [DONE])", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
		} else {
			// No content received or scanner error - genuinely truncated.
			errMsg = "stream truncated: upstream closed connection without [DONE] sentinel"
			debuglog.Warn("proxy: stream ended without [DONE] sentinel", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
		}
	}

	logData.statusCode = statusCode
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = opts.proxyOverheadMs
	logData.parseMs = opts.parseMs
	logData.failoverLookupMs = opts.failoverLookupMs
	logData.modelLookupMs = opts.modelLookupMs
	logData.providerLookupMs = opts.providerLookupMs
	logData.keyDecryptMs = opts.keyDecryptMs
	logData.dialMs = opts.dialMs
	logData.responseHeaderMs = opts.responseHeaderMs
	logData.tokensPerSecond = tps
	logData.tokensPrompt = st.promptTokens
	logData.tokensCompletion = st.completionTokens
	logData.tokensCompletionReasoning = st.reasoningTokens
	logData.tokensPromptCacheHit = st.promptCacheHitTokens
	logData.tokensPromptCacheMiss = st.promptCacheMissTokens
	logData.errorMessage = errMsg
	logData.failoverAttempt = opts.attempt
	if errMsg != "" {
		logData.statusCode = 0
		logData.state = "failed"
	} else {
		logData.state = "completed"
	}
	h.updateRequestLog(logData)

	// Record circuit breaker failure for stream stalls.
	// Guard with !sawDone to avoid penalising a provider whose stream completed
	// normally but whose stall timer fired concurrently with [DONE].
	if st.stalled && !st.sawDone && !st.clientDisconnected && opts.circuitBreakerOn {
		h.circuitBreaker.RecordFailure(opts.providerID, opts.providerName)
		debuglog.Debug("proxy: recorded circuit breaker failure for stream stall", "provider", opts.providerName, "provider_id", opts.providerID)
	}

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "duration_ms", totalDuration, "chunks", st.chunkCount, "bytes_written", sink.bytesWritten, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens, "error_chunks", st.errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "upstream_status", statusCode, "attempt", opts.attempt, "duration_ms", totalDuration)
	} else {
		debuglog.Debug("proxy: streaming completed successfully", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "duration_ms", totalDuration)
	}

	// Always record token usage against the virtual key quota, even on
	// client disconnect. The upstream provider already billed for these
	// tokens; not counting them would cause quota drift (provider bill > VK meter).
	if opts.vkHash != "" {
		if st.clientDisconnected && (st.promptTokens > 0 || st.completionTokens > 0) {
			debuglog.Info("proxy: recording token usage despite client disconnect", "model", logData.modelID, "provider", logData.providerName, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens)
		}
		h.recordTokenUsage(opts.vkHash, st.promptTokens, st.completionTokens, st.reasoningTokens, logData.virtualKeyName)
	}
}

// deriveStreamError classifies how the stream ended into the error message
// recorded on the request log, or "" when no error applies. The ladder order is
// semantic and must not be reshuffled: an in-stream SSE error wins, then the
// scanner error is classified (a context.Canceled scan marks the stream as a
// client disconnect on st), then a client disconnect overrides whatever message
// was derived so far, and finally a stall overrides the raw IO error produced
// by the watchdog's body.Close(). The missing-[DONE] diagnosis is NOT handled
// here — it may write to the client, so it stays in finalizeStream.
func deriveStreamError(st *streamState, scanErr error, opts streamOptions, logData *requestLogData) string {
	errMsg := st.lastErrMsg
	if errMsg != "" {
		// An in-stream SSE error body from the provider.
		logData.errorKind = KindProviderError
	}
	if errMsg == "" && scanErr != nil {
		switch {
		case errors.Is(scanErr, context.Canceled):
			// The scanner caught the cancellation before the select between
			// iterations could. This is always a client disconnect — the
			// parent request context was cancelled.
			st.clientDisconnected = true
		case errors.Is(scanErr, context.DeadlineExceeded):
			// A derived context's deadline expired (failover or retry timeout).
			// Use cancelOrigin to produce a human-readable message.
			switch opts.cancelOrigin {
			case "retry_timeout":
				errMsg = "stream interrupted: param-strip retry timed out"
				logData.errorKind = KindRetryTimeout
			case "failover_timeout":
				errMsg = "stream interrupted: upstream request timed out"
				logData.errorKind = KindFailoverTimeout
			default:
				// Unknown origin — preserve the value rather than guessing.
				errMsg = fmt.Sprintf("stream interrupted: %s", humanReadableCancelOrigin(opts.cancelOrigin))
				logData.errorKind = cancelOriginToKind(opts.cancelOrigin)
			}
		default:
			errMsg = scanErr.Error()
			logData.errorKind = KindProviderError
		}
	}
	if st.clientDisconnected {
		errMsg = "client disconnected"
		logData.errorKind = KindClientDisconnect
		// A client hangup is normal client behavior, not a gateway/provider
		// fault — log at Info (see the level semantics in doc/logging.md).
		debuglog.Info("proxy: client disconnected during streaming", "model", logData.modelID)
	}
	// Stall detection takes precedence over the raw IO error produced by
	// the watchdog's body.Close(). Replace it with a descriptive message.
	// Only flag a stall when we did NOT see [DONE] — if the stream completed
	// normally, a late timer fire is a false positive. Also skip when the
	// client disconnected, which is a more meaningful diagnosis.
	if st.stalled && !st.sawDone && !st.clientDisconnected {
		effectiveStall := opts.streamStallTimeout
		if st.chunkCount > progressiveChunkThreshold {
			effectiveStall = opts.streamStallTimeout * progressiveStallMultiplier
		}
		errMsg = fmt.Sprintf("stream stalled: no data for %s", effectiveStall)
		logData.errorKind = KindProviderTimeout
		debuglog.Warn("proxy: stream stall detected", "model", logData.modelID, "provider", logData.providerName, "stall_timeout", effectiveStall, "base_timeout", opts.streamStallTimeout, "chunks", st.chunkCount)
	}
	return errMsg
}
