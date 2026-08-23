package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
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
	// masker scrubs the provider's credential from every frame bound for the
	// client and from the log's error message; copied from streamOptions so
	// the chunk handlers can reach it.
	masker                credentialMasker
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
	// sawContent records that at least one non-empty content or reasoning delta
	// reached the client. It is the only signal that a stream actually answered
	// which does not depend on optional behaviour: usage chunks are omitted by
	// some providers, and the TTFT probe can be turned off.
	sawContent bool
	// deliveredBytes counts the bytes of content, reasoning and tool-call
	// arguments the model produced as it streamed (before any strip_reasoning
	// transform, which drops text the provider still billed). It backs the
	// usage estimate when no usage was reported: the usage chunk is the LAST
	// chunk of an OpenAI stream, so a client that hangs up after the content
	// (or a provider that omits the chunk) would otherwise leave the request
	// unmetered while the provider still bills for it.
	deliveredBytes     int
	clientDisconnected bool
	stalled            bool
	interrupted        bool // the process is shutting down; set from the reader

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
	// Carried for the retirement verdict, which has to decide whether the model
	// answered. On the native Anthropic passthrough the chunks are never parsed
	// into deltas, so its terminal message_stop stands in for the same fact.
	logData.deliveredContent = st.sawContent || st.sawMessageStop
	if errMsg != "" {
		h.writeTerminalError(sink, st, opts, logData, errMsg)
	}
	logData.errorMessage = string(opts.masker.mask([]byte(errMsg)))
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
	if st.stalled && !st.interrupted && !st.sawDone && !st.clientDisconnected && opts.circuitBreakerOn {
		// deriveStreamError already warns that the stream stalled; this records
		// that the breaker was charged for it, which the stall line does not say
		// and which is otherwise invisible above Debug.
		debuglog.Warn("proxy: recording circuit breaker failure", "reason", "stream stalled", "provider", opts.providerName, "provider_id", opts.providerID, "model", logData.modelID, "attempt", opts.attempt, "chunks", st.chunkCount, "duration_ms", totalDuration)
		h.circuitBreaker.RecordFailure(opts.providerID, opts.providerName)
	}

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "duration_ms", totalDuration, "chunks", st.chunkCount, "bytes_written", sink.bytesWritten, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens, "error_chunks", st.errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "upstream_status", statusCode, "attempt", opts.attempt, "duration_ms", totalDuration)
	} else {
		debuglog.Debug("proxy: streaming completed successfully", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "duration_ms", totalDuration)
	}

	// Always record token usage, even on client disconnect. The upstream
	// provider already billed for these tokens; not counting them would cause
	// quota drift (provider bill > meter). recordTokenUsage picks the meter:
	// a keyed request charges the virtual key's quota and TPM bucket, while a
	// keyless one (admin chat) has no virtual key quota to charge and debits
	// the owner's aggregate TPM bucket instead.
	if st.clientDisconnected && (st.promptTokens > 0 || st.completionTokens > 0) {
		debuglog.Info("proxy: recording token usage despite client disconnect", "model", logData.modelID, "provider", logData.providerName, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens)
	}
	// The estimate applies on failed streams too (an SSE error after some
	// output, a truncation): the provider billed whatever was produced before
	// the failure. The non-streaming path only meters a decoded 2xx, which is
	// the same rule, since nothing was produced for the client otherwise.
	promptTokens, completionTokens, reasoningTokens := estimateMissingUsage(st.promptTokens, st.completionTokens, st.reasoningTokens, logData, st.deliveredBytes)
	h.recordTokenUsage(opts.vkHash, logData, promptTokens, completionTokens, reasoningTokens)
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
		// An in-stream SSE error body from the provider. Unlike the non-streaming
		// path (forwardUpstreamError) this text never went through
		// SanitizeLogBody, so it reached request_logs uncapped and with UUIDs
		// intact — a provider is free to echo the request back inside an error,
		// and an unbounded provider string must not land in the log either way.
		errMsg = util.SanitizeLogBody(errMsg, 10000)
		logData.errorKind, _ = classifyUpstreamError(logData.statusCode, errMsg, upstreamModelID(logData))
		// Kept separately as well, because everything below can overwrite
		// errorKind with a later cause. A client that receives this error chunk
		// and hangs up — which is exactly what a client does on seeing an error
		// — would otherwise replace the provider's "this model is gone" with
		// client_disconnect, and the retirement would lose the very strike the
		// provider just handed it. What the provider said about the model does
		// not depend on what the client did next.
		logData.upstreamKind = logData.errorKind
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
	// A process shutdown closed the upstream body. It is judged before the
	// stall so a restart never reads as a provider fault.
	if st.interrupted && !st.sawDone && !st.clientDisconnected {
		errMsg = "stream interrupted: gateway restarting"
		logData.errorKind = KindInternal
		debuglog.Warn("proxy: stream interrupted by shutdown", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
	} else if st.stalled && !st.sawDone && !st.clientDisconnected {
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

// upstreamModelID is the id the PROVIDER knows this request by, which is not
// always the one the client asked for.
//
// modelID is the client's spelling: for a failover request it is the literal
// "hotel/<group>", and resolvedModelID is where the committed candidate's real
// id lands (beginAttempt sets it only on that path, which is why the fallback is
// not a convenience). Handing the alias to classifyUpstreamError meant its
// gone-phrase test looked for "claude" — modelGoneAbout trims to the last path
// segment — inside "Model claude-sonnet-4 is no longer available", and did not
// find it, so a model retired inside a failover group recorded no strike and was
// never probed.
//
// Every other classification site passes candidate.model.ModelID directly. This
// one runs at stream teardown with no candidate in hand, so it reads the same
// fact off the log entry.
func upstreamModelID(logData *requestLogData) string {
	if logData.resolvedModelID != "" {
		return logData.resolvedModelID
	}
	return logData.modelID
}

// writeTerminalError ends a stream that failed after commit with a
// well-formed error frame so the client receives an API error it can act on
// rather than a cut connection (an "incomplete chunked read" in most SDKs).
// A stream that stalled, was truncated by the upstream, or was cut by a
// gateway restart cannot fail over once bytes have gone out, so the frame is
// the graceful end. Nothing is written when the client is gone, when the
// upstream already sent its own error frame (it was forwarded, credentials
// masked), or when [DONE] went out: the terminal frame must be the one error
// the client sees. The translated path speaks OpenAI (error object, then
// [DONE]); the native Anthropic passthrough speaks Messages (an error event,
// no sentinel).
func (h *Handler) writeTerminalError(sink *streamSink, st *streamState, opts streamOptions, logData *requestLogData, errMsg string) {
	if st.clientDisconnected || st.sawDone || st.errorChunkCount > 0 {
		return
	}
	msg := string(opts.masker.mask([]byte(errMsg)))
	var frame []byte
	if opts.rawPassthrough {
		frame = append([]byte("event: error\ndata: "), anthropic.BuildErrorResponseFromMessage(msg, http.StatusBadGateway)...)
		frame = append(frame, "\n\n"...)
	} else {
		frame = buildOpenAIStreamError(msg, string(logData.errorKind))
	}
	if err := sink.write(frame); err != nil {
		debuglog.Warn("proxy: failed to write terminal error frame", "model", logData.modelID, "provider", logData.providerName, "error", err)
		return
	}
	sink.flush()
	debuglog.Info("proxy: terminal error frame sent", "model", logData.modelID, "provider", logData.providerName, "error_kind", logData.errorKind)
}

// buildOpenAIStreamError renders the OpenAI-style in-stream error the
// streaming clients already parse from providers, followed by the [DONE]
// sentinel so the stream closes cleanly.
func buildOpenAIStreamError(message, code string) []byte {
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"code":    code,
		},
	})
	if err != nil {
		return nil
	}
	return []byte("data: " + string(body) + "\n\ndata: [DONE]\n\n")
}
