package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// buildNativeAnthropicRequest builds the upstream request for the native
// Anthropic passthrough path: the original Messages body (model rewritten to the
// resolved upstream id) sent to the provider's native /v1/messages. No
// translation, so cache_control / thinking / fine-grained tool streaming survive
// upstream. Auth + anthropic-version headers come from SetProviderAuthHeaders.
func (h *Handler) buildNativeAnthropicRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	// A Gemini thought signature riding on a tool_use id (see
	// anthropic.StripSignedToolUseIDs) is dropped: this provider has no use for
	// it, and it is a kilobyte of prompt per call per turn.
	body := util.RewriteJSONModel(anthropic.StripSignedToolUseIDs(st.anthropicRawBody), candidate.model.ModelID)
	proxyReq, targetURL, err := newMessagesRequest(ctx, candidate, providerType, body)
	debuglog.Debug("proxy: native anthropic passthrough", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	return proxyReq, providerType, targetURL, nil
}

// buildNativeResponsesRequest builds the upstream request for the native
// Responses passthrough: the original /v1/responses body (model rewritten to
// the resolved upstream id) sent to OpenAI's own /v1/responses. No
// translation, so hosted tools, encrypted reasoning items, prompt_cache_key
// and every other knob survive upstream. Learned param strips do not apply,
// as on the Anthropic passthrough.
func (h *Handler) buildNativeResponsesRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	targetURL := responsesTargetURL(candidate, providerType)
	body := util.RewriteJSONModel(st.responsesRawBody, candidate.model.ModelID)
	debuglog.Debug("proxy: native responses passthrough", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name)
	proxyReq, err := newJSONUpstreamRequest(ctx, targetURL, body)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	return proxyReq, providerType, targetURL, nil
}

// handleNativeNonStreaming serves a non-streaming native success response (any
// 2xx) in the given dialect. The upstream body is already in the client's wire
// format, so it is forwarded verbatim. Token usage is read from the dialect's
// usage block for metering and quota, mirroring handleNonStreamingResponse.
//
// canFailOver says whether the group still has a candidate behind this one. A
// success whose body never arrives is a request this provider did not serve,
// so while a sibling can still be asked it is failed over to rather than
// answered with this gateway's 502, the same rule the translated path applies
// to a 2xx that is not a completion.
func (h *Handler) handleNativeNonStreaming(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, native nativeDialect, resp *http.Response, attempt int, responseHeaderMs float64, canFailOver bool) candidateOutcome {
	logData := st.logData
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()

	body, err := httpx.ReadCappedBody(resp.Body, nonStreamingBodyCap)
	if err != nil {
		// Fenced here and not only in rejectUntranslatableBody below: this line is
		// unconditional and fires first, so leaving it raw would publish the text
		// the fenced line withholds.
		debuglog.Warn("proxy: "+native.label()+" read failed",
			"error", fencedFrameMessage(logData.fence(), logData.masks(), errString(err)),
			"provider", logData.providerName)
		// The same two gates the translated path applies, from the same two
		// helpers: an abandoned attempt has nobody waiting for a second answer,
		// and a body past this gateway's own cap is not something a sibling can
		// answer any better. Everything else goes to the sibling, charged or not
		// by rejectUntranslatableBody's own rule.
		if canFailOver && !requestAbandoned(r.Context(), err) && answerFaultIsRoutable(err) {
			return h.rejectUntranslatableBody(st, candidate, logData, native.label(), resp.StatusCode, err, attempt, r)
		}
		// Finalize the log row so it does not orphan in the in-flight state. A
		// read failure on a success body is a provider or transport fault,
		// unless it was interrupted, which abortKind decides the same way the
		// translated path does: the identical event must not log provider_error
		// here and client_disconnect there.
		// A body past the cap is refused by THIS gateway, so it is reported the
		// way the translated path reports its own refusal: a bad request the
		// provider is not charged for, never a provider fault.
		kind := KindProviderError
		if aborted, isAbort, _ := abortKind(r.Context(), err); isAbort {
			kind = aborted
		} else if errors.Is(err, httpx.ErrBodyTooLarge) {
			kind = KindProviderBadRequest
		}
		logData.statusCode = http.StatusBadGateway
		logData.durationMs = util.MillisSince(st.startTime)
		logData.responseHeaderMs = responseHeaderMs
		logData.failoverAttempt = attempt
		logData.errorKind = kind
		logData.errorMessage = "failed to read upstream response: " + err.Error()
		logData.state = "failed"
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		native.writeError(w, "failed to read upstream response", http.StatusBadGateway)
		return outcomeFatal
	}
	// Exact-key scrub only: this is a success body, content where the
	// key-shape regex must not run.
	body = logData.masker.maskExact(body)

	usage := native.parseUsage(body)
	// The native prompt figure and its cache miss are sums of members the decoder
	// bounded one at a time, so they arrive unbounded. Every figure this function
	// writes is clamped, so the log row's five token columns, the estimate and
	// the charge agree.
	inputTokens, outputTokens, _ := h.clampReportedUsage(usage.promptTokens, usage.completionTokens, 0, logData)
	totalDuration := util.MillisSince(st.startTime)

	// What clears the model's gone-strike streak (see dispatchNonStreaming), so the
	// bar is content and not bytes: `200 {"content":[]}` is what an aggregator in
	// front of a retired model returns between its refusals, and crediting it
	// would stop the streak ever reaching three consecutive strikes. Tokens
	// corroborate, for a provider that answers without reporting usage. Same
	// judgement chatAnswerCarriesContent makes on the OpenAI-shaped path.
	carriesContent := outputTokens > 0 || native.carriesContent(body)
	// The failover bar, which is wider than the content one, exactly as
	// completionCarriesAnswer is wider than answerCarriesSomething on the
	// translated path. A stated stop_reason is the provider saying how its own
	// generation ended, and `{"content":[],"stop_reason":"max_tokens"}` is a
	// finished generation whoever asked for it: without this the same upstream
	// answer is served through /v1/chat/completions and routed to a sibling
	// through /v1/messages, deciding a re-billed prompt on nothing but the
	// dialect the caller used.
	answered := carriesContent || native.stopStated(body)
	// An answer carrying nothing goes to the sibling while there is one, the
	// same rule and the same bar the translated path applies (completionFault).
	// Above every stamp below, because a candidate the loop is about to leave
	// must write neither a completed row nor a token charge for an answer the
	// client will never see.
	if canFailOver && !answered {
		// Charged before the candidate is left behind: the provider read this
		// prompt and billed it, whoever ends up serving the request.
		h.meterRejectedPrompt(st, logData, candidate, inputTokens, usage.cacheHitTokens, usage.cacheMissTokens)
		return h.rejectUntranslatableBody(st, candidate, logData, native.label(), resp.StatusCode, errEmptyCompletion, attempt, r)
	}

	// The status the provider actually sent, not a flattened 200: a relay may
	// answer a native message 201, and recording 200 would put a number in the
	// request log that no upstream ever returned.
	logData.statusCode = resp.StatusCode
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = st.proxyOverhead
	logData.parseMs = st.parseMs
	logData.applyTimings(st.timings)
	logData.responseHeaderMs = responseHeaderMs
	// Added, not assigned: an earlier candidate that answered 2xx without an
	// answer already billed its prompt onto this row (meterRejectedPrompt), and
	// the row reports what the request cost rather than what its last hop did.
	logData.tokensPrompt += inputTokens
	logData.tokensCompletion = outputTokens
	// The cache split, not just the total: metering the cache-inclusive prompt
	// without it prices every cached token at full input rate. The translated
	// path reaches the same two fields through extractCacheTokens. Added like
	// the prompt total, so a rejected earlier candidate's split survives.
	logData.tokensPromptCacheHit += clampTokenCount(usage.cacheHitTokens)
	logData.tokensPromptCacheMiss += clampTokenCount(usage.cacheMissTokens)
	logData.failoverAttempt = attempt
	logData.state = "completed"
	logData.deliveredContent = carriesContent
	// The question the breaker asks of the same body: did anything come back.
	// ResponseCarriesContent reads block PRESENCE, which is the native analogue
	// of the translated path's "any choice carrying something", so on this path
	// the two bars coincide and the negation is exact.
	logData.emptyCompletion = !carriesContent
	// The estimate runs before the terminal write so the row is priced by
	// what the provider billed, not by the usage it left out.
	inputTokens, outputTokens, _ = estimateMissingUsage(inputTokens, outputTokens, 0, logData, native.textBytes(body))
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	h.recordTokenUsage(st.vkHash, logData, inputTokens, outputTokens, 0)

	debuglog.Info("proxy: "+native.label()+" non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "duration_ms", totalDuration, "input_tokens", inputTokens, "output_tokens", outputTokens)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	// #nosec G705 -- native JSON response body (Anthropic message or Responses object), not HTML; Content-Type is application/json
	_, _ = w.Write(body)
	return outcomeServed
}

// emitRawData forwards one streaming data chunk verbatim on a native
// passthrough, mirroring the no-transform branch of handleDataChunk. From the
// same decode it meters token usage, records the dialect's terminal event so
// finalizeStream can tell a real completion from a mid-stream truncation, and
// captures a provider-sent error event into streamState so the request logs as
// failed (deriveStreamError surfaces st.lastErrMsg) rather than "completed". The
// error frame is still forwarded to the client, with the provider's credential
// masked by the same credentialMasker scrub handleDataChunk applies: the exact
// key on every event, key shapes on errors.
//
// Returns stop=true on a client write failure.
func (h *Handler) emitRawData(sink *streamSink, st *streamState, native nativeDialect, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	info := native.inspectStreamEvent([]byte(ev.payload))
	// Judged like the translated path's observer, which REFUSES an out-of-range
	// member rather than clamping it: on a stream there is an earlier reading to
	// keep and an estimator to fall back on, so a chunk saying something absurd
	// says nothing. Clamping here instead would let the figure that is discarded
	// on an OpenAI-shaped stream charge the ceiling on this one.
	if info.hasInput && isTokenReading(info.inputTokens) {
		st.promptTokens = info.inputTokens
		// Guarded like the translated path's observer: a later usage event
		// without cache fields must not zero a split an earlier one reported.
		// The split is clamped rather than refused, matching extractCacheTokens:
		// these two are log columns, not a charge.
		if info.cacheHitTokens > 0 || info.cacheMissTokens > 0 {
			st.promptCacheHitTokens = clampTokenCount(info.cacheHitTokens)
			st.promptCacheMissTokens = clampTokenCount(info.cacheMissTokens)
		}
	}
	if info.hasOutput && isTokenReading(info.outputTokens) {
		st.completionTokens = info.outputTokens
	}
	st.deliveredBytes += info.textBytes
	if info.hasSequence {
		st.nativeSequence = info.sequenceNumber + 1
	}
	if info.responseID != "" {
		st.nativeResponseID = info.responseID
	}
	if info.terminal {
		st.sawTerminalEvent = true
	}
	// A Responses stream reports a failed generation on its terminal
	// response.failed event, so the error is read wherever it rides.
	if info.errorMessage != "" {
		st.lastErrMsg = info.errorMessage
		st.errorChunkCount++
		debuglog.Warn("proxy: "+native.label()+" SSE error event", "event", info.eventType, "error_message", st.errLogAttr(info.errorMessage), "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
	}
	line := ev.raw
	masked := st.masker.maskExact([]byte(ev.payload))
	if info.eventType == "error" || info.carriesError {
		// Error text, by the wrapper's type or by a populated error member on
		// any event (the same rule the translated path applies): every held
		// provider key and the shape layer.
		masked = st.masker.mask(masked)
	}
	if string(masked) != ev.payload {
		line = append([]byte("data: "), masked...)
	}
	if err := sink.write(line); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during native stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	if err := sink.write([]byte("\n\n")); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during native stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
		return true
	}
	sink.flush()
	sink.swallowBlank = true
	return false
}
