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
	// unparsedChunks counts data frames this gateway could not read: bytes that
	// are not well-formed JSON (dropped), a shape its types do not cover
	// (forwarded verbatim), and the frames dropped because strip_reasoning could
	// not be applied to one. Whether the provider actually answered in them is
	// unknowable from here, so they are neither evidence of emptiness nor
	// evidence against it — see judgeStreamForBreaker, which reads delivery
	// FIRST and falls back to this only when nothing typed reached the caller.
	unparsedChunks int
	lastErrMsg     string
	sawDone        bool
	sawMessageStop bool // native Anthropic passthrough: terminal message_stop event seen
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
	// clientErrMsg overrides the client-facing terminal frame message when the
	// derived errMsg carries provider/transport detail that must not leave the
	// gateway (a raw scanner error can embed internal IPs and the upstream
	// address). Empty means the client frame reuses errMsg, which is
	// gateway-authored on every other failure path.
	clientErrMsg string

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

// streamBreakerVerdict is what a finished stream tells the circuit breaker about
// its provider: charge a failure (naming why, for the operator log), credit a
// success, or say nothing at all.
type streamBreakerVerdict struct {
	failureReason string
	success       bool
}

// providerAtFault reports whether an error kind is the provider's to answer for.
// The gateway's own timeouts, its internal cancels and a client hangup are not,
// and must never darken a provider that was doing nothing wrong.
func providerAtFault(kind ErrorKind) bool {
	switch kind {
	case KindProviderError, KindProviderTimeout, KindProviderModelGone:
		return true
	default:
		return false
	}
}

// streamDeliveredOutput reports whether the caller actually received model
// output, from the signals that answer that question and only those:
//
//   - st.sawContent — a non-empty content or reasoning delta reached the client.
//   - st.deliveredBytes — the bytes of content, reasoning, reasoning_details and
//     tool-call arguments the model produced. Counted on BOTH the translated and
//     the native Anthropic path, so it is the one signal that catches a native
//     stream which really did deliver.
//   - st.completionTokens — the provider reported usage even if this gateway
//     never parsed content out of the deltas.
//
// Deliberately NOT logData.deliveredContent, and deliberately NOT
// st.sawMessageStop. deliveredContent is derived as sawContent||sawMessageStop
// for the RETIREMENT verdict, where a terminal message_stop is allowed to stand
// in for "the model answered". Here it cannot: message_stop is a TERMINATION
// signal, present on every native stream that ends cleanly, including one that
// ended having produced nothing. Reading it as delivery is what let a completely
// empty /v1/messages response escape the charge entirely.
//
// A tool call is output. So is reasoning. The cost of missing one of these is a
// charge against a provider that answered correctly, which after five requests
// takes it out of rotation for every tenant — so this errs toward "delivered".
func streamDeliveredOutput(st *streamState) bool {
	return st.sawContent || st.deliveredBytes > 0 || st.completionTokens > 0
}

// judgeStreamForBreaker decides what a finished stream tells the circuit
// breaker. It is the whole policy for a streaming 200, in one place.
//
// Why the verdict lives HERE rather than at the TTFT probe: recordBreakerOutcome
// deliberately says nothing for a streaming 200, so the probe was the only voice
// these requests had — and it spoke too early. A first token is not a served
// stream. Recording success there zeroed consecutiveFails on every single
// request, so a provider that failed AFTER the first token oscillated 0→1 against
// a threshold of 5 and could never open its circuit. Both charges below were
// therefore inert in production. Move a success back to the probe and they go
// inert again.
//
// Failure has three shapes. A stall is the provider going silent; an error is
// the provider saying it failed; an empty finish is the provider completing
// having said nothing at all. The stall charge is unconditional (as it has
// always been); the other two are charged only when the caller received
// nothing, because a stream that delivered real output before dying did part of
// its job and charging it would break the circuit on every
// truncated-but-useful response.
//
// The error charge matters because the probe has already committed to this
// provider by the time a later frame turns out to be an error, and the hedged
// rivals were cancelled when it did (probeFirstToken keeps an error in the FIRST
// frame from ever winning that race). The breaker is then the only thing left
// that can keep the next request away.
func judgeStreamForBreaker(st *streamState, logData *requestLogData, errMsg string, circuitBreakerOn bool) streamBreakerVerdict {
	if !circuitBreakerOn || st.interrupted || st.clientDisconnected {
		return streamBreakerVerdict{}
	}
	if errMsg == "" {
		// A clean finish still has to have finished something. A stream that
		// got past the probe, completed without error and handed the caller
		// nothing is a completely empty response, and counts against the
		// provider rather than clearing its failures — which is what recording
		// a success here used to do.
		//
		// The probe catches the stream that never produced a chunk; this is its
		// sibling, the one that produced frames carrying no output and was
		// already committed to by the time that became clear.
		//
		// Delivery is read FIRST, and unparsedChunks only decides what to do
		// when nothing typed arrived. Read the other way round, one frame this
		// gateway could not parse withheld the CREDIT from a stream that plainly
		// answered — so a provider whose shapes it does not model never cleared
		// its consecutive-failure count, and old failures accumulated until an
		// unrelated one opened the circuit.
		if streamDeliveredOutput(st) {
			// Includes the stream whose absent [DONE] was injected above: the
			// sentinel was missing, the answer was not.
			return streamBreakerVerdict{success: true}
		}
		// Nothing typed reached the caller, and frames went unread. Those were
		// unreadable to THIS gateway's parser, not left out by the provider, so
		// emptiness cannot be pinned on the upstream. Recording nothing is the
		// honest verdict — neither a charge nor a credit. Calling them delivery
		// instead was worse than either: a relay that numbers its stop reasons
		// emits one untypeable frame per stream, on the terminal chunk, which
		// carries no output at all — so every empty response it gave would have
		// cleared its failure streak, and its circuit could never open.
		if st.unparsedChunks > 0 {
			return streamBreakerVerdict{}
		}
		return streamBreakerVerdict{failureReason: "stream completed without delivering content"}
	}
	// Reached only with a non-empty errMsg, so errorKind is always set:
	// deriveStreamError writes the two together. The clean-finish charge above
	// sits ABOVE this gate deliberately — it has no errMsg and therefore no
	// kind, and the two non-provider causes that could reach it (interrupted,
	// clientDisconnected) are already short-circuited at the top.
	if !providerAtFault(logData.errorKind) {
		return streamBreakerVerdict{}
	}
	// !sawDone/!sawMessageStop avoids penalising a provider whose stream
	// completed normally but whose stall timer fired concurrently with the
	// terminal frame.
	if st.stalled && !st.sawDone && !st.sawMessageStop {
		return streamBreakerVerdict{failureReason: "stream stalled"}
	}
	if !streamDeliveredOutput(st) {
		return streamBreakerVerdict{failureReason: "stream failed without delivering content"}
	}
	return streamBreakerVerdict{}
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
			logData.errorKind = KindProviderError
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
	//
	// deliveredBytes, not sawContent alone: sawContent watches content and
	// reasoning on choices[0], so a stream whose whole answer is a tool call, or
	// one delivered in a shape this gateway forwarded without being able to
	// type, read as having produced nothing — and the streak a real answer
	// should have cleared stayed on the model. It is the same bar the breaker
	// uses (streamDeliveredOutput), and it can only ever turn an inconclusive
	// verdict into a served one: verdictForStream decides gone from the error
	// kind, before it looks at this at all.
	logData.deliveredContent = st.sawContent || st.sawMessageStop || st.deliveredBytes > 0
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

	// deriveStreamError already warns about the stall or the error itself; the
	// line below records that the breaker was CHARGED for it, which those do not
	// say and which is otherwise invisible above Debug.
	if verdict := judgeStreamForBreaker(st, logData, errMsg, opts.circuitBreakerOn); verdict.failureReason != "" {
		debuglog.Warn("proxy: recording circuit breaker failure", "reason", verdict.failureReason, "provider", opts.providerName, "provider_id", opts.providerID, "model", logData.modelID, "attempt", opts.attempt, "chunks", st.chunkCount, "error_chunks", st.errorChunkCount, "duration_ms", totalDuration)
		h.circuitBreaker.RecordFailure(opts.providerID, opts.providerName)
	} else if verdict.success {
		h.circuitBreaker.RecordSuccess(opts.providerID, opts.providerName)
	}

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "duration_ms", totalDuration, "chunks", st.chunkCount, "bytes_written", sink.bytesWritten, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens, "error_chunks", st.errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		// errLogAttr, not errMsg: the row above it is masked, the client frame
		// beside it is masked, and this line was the one place the provider's
		// raw text — a relayed credential included — reached the app log.
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerName, "error", st.errLogAttr(errMsg), "upstream_status", statusCode, "attempt", opts.attempt, "duration_ms", totalDuration)
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
			// The raw scanner error can embed the gateway's own address and the
			// upstream's: keep it in the log, hand the client a coarse message.
			st.clientErrMsg = "stream failed: upstream connection error"
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
	if st.interrupted && !st.sawDone && !st.sawMessageStop && !st.clientDisconnected {
		errMsg = "stream interrupted: gateway restarting"
		logData.errorKind = KindInternal
		debuglog.Warn("proxy: stream interrupted by shutdown", "model", logData.modelID, "provider", logData.providerName, "chunks", st.chunkCount)
	} else if st.stalled && !st.sawDone && !st.sawMessageStop && !st.clientDisconnected {
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
// upstream already sent an error the client saw (errorChunkCount>0; note a
// provider that split its error object across data lines has that counter set
// even though the fragments were dropped, so that rare stream still ends
// without a frame), or when [DONE] / a native message_stop went out: the
// terminal frame must be the one error the client sees. The translated path speaks OpenAI (error object, then
// [DONE]); the native Anthropic passthrough speaks Messages (an error event,
// no sentinel).
func (h *Handler) writeTerminalError(sink *streamSink, st *streamState, opts streamOptions, logData *requestLogData, errMsg string) {
	if st.clientDisconnected || st.sawDone || st.sawMessageStop || st.errorChunkCount > 0 {
		return
	}
	clientMsg := errMsg
	if st.clientErrMsg != "" {
		clientMsg = st.clientErrMsg
	}
	msg := string(opts.masker.mask([]byte(clientMsg)))
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
