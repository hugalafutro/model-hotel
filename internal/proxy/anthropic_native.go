package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// buildNativeAnthropicRequest builds the upstream request for the native
// Anthropic passthrough path: the original Messages body (model rewritten to the
// resolved upstream id) sent to the provider's native /v1/messages. No
// translation, so cache_control / thinking / fine-grained tool streaming survive
// upstream. Auth + anthropic-version headers come from SetProviderAuthHeaders.
func (h *Handler) buildNativeAnthropicRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/messages")
	body := anthropic.RewriteModel(st.anthropicRawBody, candidate.model.ModelID)
	debuglog.Debug("proxy: native anthropic passthrough", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name)

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// handleNativeNonStreaming serves a non-streaming native Anthropic 200 response:
// the upstream body is already an Anthropic message, so it is forwarded verbatim
// (through the verbatim-mode response writer). Token usage is read from the
// Anthropic usage block for metering + quota, mirroring handleNonStreamingResponse.
func (h *Handler) handleNativeNonStreaming(w http.ResponseWriter, r *http.Request, st *requestState, resp *http.Response, attempt int, responseHeaderMs float64) candidateOutcome {
	logData := st.logData
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debuglog.Warn("proxy: native anthropic read failed", "error", err, "provider", logData.providerName)
		// Finalize the log row so it does not orphan in the in-flight state: a
		// read failure on a 200 body is a provider/transport fault.
		logData.statusCode = http.StatusBadGateway
		logData.durationMs = float64(time.Since(st.startTime).Microseconds()) / 1000.0
		logData.responseHeaderMs = responseHeaderMs
		logData.failoverAttempt = attempt
		logData.errorKind = KindProviderError
		logData.errorMessage = "failed to read upstream response: " + err.Error()
		logData.state = "failed"
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		writeAnthropicError(w, "failed to read upstream response", http.StatusBadGateway)
		return outcomeFatal
	}

	inputTokens, outputTokens := anthropic.ParseResponseUsage(body)
	totalDuration := float64(time.Since(st.startTime).Microseconds()) / 1000.0

	logData.statusCode = http.StatusOK
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = st.proxyOverhead
	logData.parseMs = st.parseMs
	logData.failoverLookupMs = st.timings.failoverLookupMs
	logData.modelLookupMs = st.timings.modelLookupMs
	logData.providerLookupMs = st.timings.providerLookupMs
	logData.keyDecryptMs = st.timings.keyDecryptMs
	logData.dialMs = st.timings.dialMs
	logData.settingsReadMs = st.timings.settingsReadMs
	logData.responseHeaderMs = responseHeaderMs
	logData.tokensPrompt = inputTokens
	logData.tokensCompletion = outputTokens
	logData.failoverAttempt = attempt
	logData.state = "completed"
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

	if st.vkHash != "" {
		h.recordTokenUsage(st.vkHash, inputTokens, outputTokens, 0, logData.virtualKeyName)
	}

	debuglog.Info("proxy: native anthropic non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "duration_ms", totalDuration, "input_tokens", inputTokens, "output_tokens", outputTokens)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	//nolint:gosec // G705 false positive: Anthropic JSON response body, not HTML; Content-Type is application/json
	_, _ = w.Write(body)
	return outcomeServed
}

// emitRawData forwards one streaming data chunk verbatim (native Anthropic
// passthrough), mirroring the no-transform branch of handleDataChunk. From the
// same decode it (1) meters token usage, (2) records the terminal message_stop
// so finalizeStream can tell a real completion from a mid-stream truncation, and
// (3) captures a provider-sent error event into streamState so the request logs
// as failed (deriveStreamError surfaces st.lastErrMsg) rather than silently
// "completed" — the error frame is still forwarded to the client too. Returns
// stop=true on a client write failure.
func (h *Handler) emitRawData(sink *streamSink, st *streamState, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	info := anthropic.InspectStreamEvent([]byte(ev.payload))
	if info.HasInput {
		st.promptTokens = info.InputTokens
	}
	if info.HasOutput {
		st.completionTokens = info.OutputTokens
	}
	switch info.Type {
	case "message_stop":
		st.sawMessageStop = true
	case "error":
		if info.ErrorMessage != "" {
			st.lastErrMsg = info.ErrorMessage
			st.errorChunkCount++
			debuglog.Warn("proxy: native anthropic SSE error event", "error_message", info.ErrorMessage, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
		}
	}
	if err := sink.write(ev.raw); err != nil {
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
