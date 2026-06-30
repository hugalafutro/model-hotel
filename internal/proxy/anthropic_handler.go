package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Messages serves the native Anthropic Messages API surface (POST /v1/messages).
// It translates the Anthropic request to the OpenAI chat-completions shape the
// gateway speaks internally, runs the exact same ingest -> resolve -> failover
// pipeline as ChatCompletions (so routing, failover, hedging, metering, the TTFT
// probe, and the stall watchdog all apply unchanged), and wraps the writer so all
// output — streaming SSE, the non-streaming response, and every error — is
// converted back to the Anthropic wire format. Model routing is identical to the
// OpenAI side: the client must send "provider/model" or "hotel/group".
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	rawBody, ok := h.readAnthropicBody(w, r)
	if !ok {
		return
	}

	openaiBody, model, stream, err := anthropic.TranslateRequest(rawBody)
	if err != nil {
		debuglog.Warn("anthropic: request translation failed", "error", err)
		writeAnthropicError(w, err.Error(), http.StatusBadRequest)
		return
	}

	messageID := "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	aw := newAnthropicResponseWriter(w, messageID, model)

	// Re-point the standard pipeline at the translated OpenAI body. model and
	// stream already match what the timeout middleware extracted from the
	// Anthropic body's top-level fields, so only the body bytes need overriding.
	ctx := context.WithValue(r.Context(), ctxkeys.RequestBodyKey, openaiBody)
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, model)
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, stream)
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewReader(openaiBody))

	st, ok := h.ingestRequest(aw, r, endpointTypeMessages)
	if !ok {
		aw.Finalize()
		return
	}
	candidates, ok := h.resolveCandidates(aw, r, st)
	if !ok {
		aw.Finalize()
		return
	}
	h.loadFailoverConfig(r, st)

	debuglog.Debug("anthropic: messages resolved (pre-loop)", "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "stream", st.isStreaming)

	if st.hedgingEnabled && st.isStreaming && len(candidates) > 1 {
		h.runHedgedStreaming(aw, r, st, candidates, h.probeStreamingCandidate)
	} else {
		h.runFailoverLoop(aw, r, st, candidates, h.attemptCandidate)
	}
	aw.Finalize()
}

// readAnthropicBody returns the raw request body, preferring the copy the
// timeout middleware cached in context (so we do not consume r.Body twice). On a
// read failure it writes an Anthropic-shaped 400 and returns ok=false.
func (h *Handler) readAnthropicBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok && len(cached) > 0 {
		return cached, true
	}
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		debuglog.Warn("anthropic: failed to read request body", "error", err)
		writeAnthropicError(w, "failed to read request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// writeAnthropicError writes an Anthropic-shaped error response. Used only for
// the early guards before the wrapping writer takes over (body read / request
// translation); all pipeline errors flow through anthropicResponseWriter.
func writeAnthropicError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:gosec // G705 false positive: Anthropic JSON error body, not HTML; Content-Type is application/json
	_, _ = w.Write(anthropic.BuildErrorResponseFromMessage(message, status))
}
