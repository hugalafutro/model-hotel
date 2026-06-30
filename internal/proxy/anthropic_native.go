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
// passthrough), mirroring the no-transform branch of handleDataChunk, while
// scanning the Anthropic usage events for best-effort metering. Returns
// stop=true on a client write failure.
func (h *Handler) emitRawData(sink *streamSink, st *streamState, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	if in, hasIn, out, hasOut := anthropic.ScanStreamUsage([]byte(ev.payload)); hasIn || hasOut {
		if hasIn {
			st.promptTokens = in
		}
		if hasOut {
			st.completionTokens = out
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
