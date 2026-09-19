package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Responses serves the OpenAI Responses API surface (POST /v1/responses), the
// wire format Responses-only clients such as Codex CLI speak. It translates
// the request to the chat-completions shape the gateway speaks internally,
// runs the exact same ingest -> resolve -> failover pipeline as ChatCompletions
// (routing, failover, metering, the TTFT probe and the stall watchdog all apply
// unchanged), and wraps the writer so all output, streaming SSE, the
// non-streaming response and every error, is converted back to the Responses
// wire format. Model routing is identical to the rest of the proxy: the client
// sends "provider/model" or "hotel/group". A candidate that is OpenAI itself is
// served the original body on its own /v1/responses (see
// buildNativeResponsesRequest); every other candidate is translated.
func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	rawBody, ok := h.readRawBody(w, r)
	if !ok {
		return
	}

	tr, err := openairesponses.TranslateRequestToChat(rawBody)
	if err != nil {
		// The rejection names the field and never the content; the decode
		// error is jsonfault's content-free description.
		var rejected *openairesponses.RejectedRequest
		if !errors.As(err, &rejected) {
			debuglog.Warn("responses: request translation failed", "error", err)
		}
		writeOpenAIError(w, err.Error(), http.StatusBadRequest)
		return
	}

	responseID := openairesponses.NewResponseID()
	aw := newResponsesResponseWriter(w, responseID, tr.Model, tr.Facts)

	// Re-point the standard pipeline at the translated chat body. model and
	// stream already match what the timeout middleware extracted from the
	// Responses body's top-level fields, so only the body bytes need overriding.
	ctx := context.WithValue(r.Context(), ctxkeys.RequestBodyKey, tr.ChatBody)
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, tr.Model)
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, tr.Stream)
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewReader(tr.ChatBody))

	st, ok := h.ingestRequest(aw, r, endpointTypeResponses)
	if !ok {
		aw.Finalize()
		return
	}
	// Mark this as Responses-in and stash the original body so an OpenAI
	// candidate can be served the native /v1/responses passthrough; bind the
	// writer to the per-attempt native flag so it forwards verbatim when so.
	st.responsesIn = true
	st.responsesRawBody = rawBody
	aw.bindNativeFlag(&st.responsesNativeAttempt)
	candidates, ok := h.resolveCandidates(aw, r, st)
	if !ok {
		aw.Finalize()
		return
	}
	// A member only OpenAI's own endpoint can serve (a hosted or custom tool, a
	// file id) is fine when every candidate is that endpoint, and a 400 naming
	// the member when any candidate would get the chat translation without it.
	// Decided here, after resolution, because the mode is per candidate.
	if tr.NativeOnly != nil && !allNativeResponses(candidates) {
		h.rejectIngest(aw, st.logData, tr.NativeOnly.Error(), st.startTime, st.parseMs)
		aw.Finalize()
		return
	}
	h.loadFailoverConfig(r, st)

	debuglog.Debug("responses: resolved (pre-loop)", "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "stream", st.isStreaming)

	// Responses requests never hedge, for the reason Messages never do: hedging
	// probes per-attempt COPIES of requestState, so a native winner would set
	// responsesNativeAttempt only on its copy and the flag would never reach
	// this request's writer, which would mis-parse the native Responses SSE as
	// OpenAI chunks. The sequential loop reads the flag on the st the writer is
	// bound to.
	h.runFailoverLoop(aw, r, st, candidates, h.attemptCandidate)
	aw.Finalize()
}

// readRawBody returns the raw request body, preferring the copy the timeout
// middleware cached in context (so we do not consume r.Body twice). On a read
// failure it writes an OpenAI-shaped 400 and returns ok=false.
func (h *Handler) readRawBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok && len(cached) > 0 {
		return cached, true
	}
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		debuglog.Warn("responses: failed to read request body", "error", err)
		writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// allNativeResponses reports whether every candidate would be served the
// native Responses passthrough (the gate buildCandidateRequest applies).
func allNativeResponses(candidates []modelCandidate) bool {
	for _, c := range candidates {
		if provider.TypeOf(c.provider) != "openai" || !isOpenAIHost(c.provider.BaseURL) {
			return false
		}
	}
	return len(candidates) > 0
}
