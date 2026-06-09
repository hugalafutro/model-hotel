package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Multimodal proxy endpoints: OpenAI-compatible pass-through for embeddings,
// image generation/edits/variations, text-to-speech, and speech-to-text.
//
// These endpoints reuse the chat pipeline phases (ingest, resolve, failover
// config, failover loop) but replace the chat-specific per-attempt dispatch
// with a transparent pass-through: the upstream response is forwarded to the
// client verbatim (JSON, SSE, or binary), with only token usage metadata
// extracted for metering. No request or response content is ever logged.

// Embeddings proxies OpenAI-compatible POST /v1/embeddings requests.
func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/embeddings", endpointTypeEmbeddings)
}

// ImageGenerations proxies OpenAI-compatible POST /v1/images/generations
// requests, including SSE streaming via the partial_images parameter.
func (h *Handler) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/images/generations", endpointTypeImage)
}

// ImageEdits proxies OpenAI-compatible POST /v1/images/edits requests
// (multipart: image file(s) + prompt + model).
func (h *Handler) ImageEdits(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/images/edits", endpointTypeImage)
}

// ImageVariations proxies OpenAI-compatible POST /v1/images/variations
// requests (multipart: image file + model).
func (h *Handler) ImageVariations(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/images/variations", endpointTypeImage)
}

// AudioSpeech proxies OpenAI-compatible POST /v1/audio/speech requests.
// The response is binary audio (or SSE when stream_format=sse) and is
// streamed through without buffering.
func (h *Handler) AudioSpeech(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/audio/speech", endpointTypeTTS)
}

// AudioTranscriptions proxies OpenAI-compatible POST /v1/audio/transcriptions
// requests (multipart: audio file + model + optional params).
func (h *Handler) AudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/audio/transcriptions", endpointTypeSTT)
}

// AudioTranslations proxies OpenAI-compatible POST /v1/audio/translations
// requests (multipart, same shape as transcriptions, always English output).
func (h *Handler) AudioTranslations(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/audio/translations", endpointTypeSTT)
}

// serveJSONPassthrough handles a JSON-bodied multimodal endpoint: reuse the
// chat ingest phase (the body carries a `model` field like chat does), attach
// the endpoint path and the JSON model rewriter, and run the shared
// pass-through pipeline.
func (h *Handler) serveJSONPassthrough(w http.ResponseWriter, r *http.Request, endpointPath, endpointType string) {
	st, ok := h.ingestRequest(w, r, endpointType)
	if !ok {
		return
	}
	st.endpointPath = endpointPath
	st.makeUpstreamBody = makeJSONModelRewriter(st.bodyBytes, st.reqModel)
	h.servePassthroughPipeline(w, r, st)
}

// serveMultipartPassthrough handles a multipart-bodied multimodal endpoint
// (audio transcription/translation, image edits/variations): parse the form
// once, extract `model`, and rebuild the form per failover candidate with the
// resolved upstream model ID substituted.
func (h *Handler) serveMultipartPassthrough(w http.ResponseWriter, r *http.Request, endpointPath, endpointType string) {
	st, parts, ok := h.ingestMultipartRequest(w, r, endpointType)
	if !ok {
		return
	}
	st.endpointPath = endpointPath
	st.makeUpstreamBody = func(resolvedModelID string) ([]byte, string, error) {
		return rebuildMultipartBody(parts, resolvedModelID)
	}
	h.servePassthroughPipeline(w, r, st)
}

// servePassthroughPipeline runs phases B-E for a multimodal request: resolve
// candidates (failover groups, Provider/model syntax, allowed_providers
// filter), load the failover config, and drive the shared failover loop with
// the pass-through attempt fn.
func (h *Handler) servePassthroughPipeline(w http.ResponseWriter, r *http.Request, st *requestState) {
	candidates, ok := h.resolveCandidates(w, r, st)
	if !ok {
		return
	}
	h.loadFailoverConfig(r, st)
	debuglog.Debug("proxy: model resolved (pre-loop)", "endpoint", st.logData.endpointType, "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "overhead_ms", st.proxyOverhead)
	h.runFailoverLoop(w, r, st, candidates, h.attemptPassthroughCandidate)
}

// makeJSONModelRewriter returns a makeUpstreamBody fn that rewrites only the
// `model` field of a JSON body to the resolved upstream model ID, forwarding
// everything else untouched. An unparseable body is forwarded as-is, mirroring
// chat's buildUpstreamBody behavior.
func makeJSONModelRewriter(body []byte, requestModel string) func(string) ([]byte, string, error) {
	return func(resolvedModelID string) ([]byte, string, error) {
		out := body
		if requestModel != resolvedModelID {
			// Best-effort rewrite: an unparseable body is forwarded as-is
			// (mirrors buildUpstreamBody).
			var raw map[string]interface{}
			if json.Unmarshal(body, &raw) == nil {
				raw["model"] = resolvedModelID
				if rewritten, err := json.Marshal(raw); err == nil {
					out = rewritten
				}
			}
		}
		return out, "application/json", nil
	}
}

// multipartPart is one parsed part of a multipart/form-data body, retained so
// the form can be rebuilt per failover candidate with the model substituted.
type multipartPart struct {
	fieldName   string
	fileName    string
	contentType string
	data        []byte
}

// parseMultipartParts decomposes a multipart body into its parts and returns
// them together with the value of the `model` form field (empty if absent).
func parseMultipartParts(body []byte, boundary string) ([]multipartPart, string, error) {
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var parts []multipartPart
	model := ""
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		data, err := io.ReadAll(p)
		_ = p.Close()
		if err != nil {
			return nil, "", err
		}
		part := multipartPart{
			fieldName:   p.FormName(),
			fileName:    p.FileName(),
			contentType: p.Header.Get("Content-Type"),
			data:        data,
		}
		if part.fieldName == "model" && part.fileName == "" {
			model = strings.TrimSpace(string(data))
		}
		parts = append(parts, part)
	}
	return parts, model, nil
}

// multipartQuoteEscaper escapes quotes and backslashes in multipart header
// values, matching the escaping used by mime/multipart.Writer.
var multipartQuoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// rebuildMultipartBody reassembles a multipart/form-data body from parsed
// parts with the `model` field replaced by the resolved upstream model ID.
// A fresh boundary is generated; the returned content type carries it.
func rebuildMultipartBody(parts []multipartPart, resolvedModelID string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, part := range parts {
		data := part.data
		if part.fieldName == "model" && part.fileName == "" {
			data = []byte(resolvedModelID)
		}
		if part.fileName == "" && part.contentType == "" {
			if err := mw.WriteField(part.fieldName, string(data)); err != nil {
				return nil, "", err
			}
			continue
		}
		hdr := make(textproto.MIMEHeader)
		disposition := fmt.Sprintf(`form-data; name="%s"`, multipartQuoteEscaper.Replace(part.fieldName))
		if part.fileName != "" {
			disposition += fmt.Sprintf(`; filename="%s"`, multipartQuoteEscaper.Replace(part.fileName))
		}
		hdr.Set("Content-Disposition", disposition)
		if part.contentType != "" {
			hdr.Set("Content-Type", part.contentType)
		}
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, "", err
		}
		if _, err := pw.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// ingestMultipartRequest performs phase A for multipart endpoints: read the
// (middleware-cached) body, parse the multipart form, extract the `model`
// field, create the early "pending" request-log entry, publish the
// request.started event, and run the early-failure guards.
//
// On success it returns the request state plus the parsed parts (for the
// per-candidate rebuild). On any guard failure it records the failure, writes
// the OpenAI error response, and returns (nil, nil, false).
func (h *Handler) ingestMultipartRequest(w http.ResponseWriter, r *http.Request, endpointType string) (*requestState, []multipartPart, bool) {
	startTime := time.Now()

	vkName := ""
	var vkID string
	var vkHash string
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName, _ = v.(string)
	}
	if v := r.Context().Value(virtualKeyIDKey); v != nil {
		vkID, _ = v.(string)
	}
	if v := r.Context().Value(VirtualKeyHashKey); v != nil {
		vkHash, _ = v.(string)
	}

	// Create the log entry early so early-return paths can record failures.
	// modelID gets updated after the multipart form is parsed.
	logData := &requestLogData{
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		failoverAttempt: 0,
		state:           "pending",
		endpointType:    endpointType,
	}
	h.insertRequestLogAsync(logData)

	parseStart := time.Now()
	var bodyBytes []byte
	if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
		bodyBytes = cached
	} else {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			debuglog.Warn("proxy: failed to read multipart request body", "error", err)
			publishRequestStartedEvent(logData)
			h.failRequest(logData, 400, "failed to read request body", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
			writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
			return nil, nil, false
		}
		_ = r.Body.Close()
	}

	mediaType, ctParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || ctParams["boundary"] == "" {
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, "Content-Type must be multipart/form-data with a boundary", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "Content-Type must be multipart/form-data with a boundary", http.StatusBadRequest)
		return nil, nil, false
	}

	parts, reqModel, err := parseMultipartParts(bodyBytes, ctParams["boundary"])
	if err != nil {
		debuglog.Warn("proxy: failed to parse multipart form", "error", err)
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, "invalid multipart form", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "invalid multipart form", http.StatusBadRequest)
		return nil, nil, false
	}
	parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

	logData.modelID = reqModel
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, nil, false
	}

	debuglog.Info("proxy: multipart request start", "endpoint", endpointType, "model", reqModel, "key", vkName, "parts", len(parts), "client_ip", r.RemoteAddr)

	return &requestState{
		startTime: startTime,
		reqModel:  reqModel,
		vkHash:    vkHash,
		bodyBytes: bodyBytes,
		parseMs:   parseMs,
		logData:   logData,
	}, parts, true
}

// attemptPassthroughCandidate runs one failover attempt for a multimodal
// request: build and send the upstream request, record the circuit-breaker
// outcome, and either fail over to the next candidate, forward a terminal
// error, or stream the response through. It is the multimodal counterpart of
// attemptCandidate, without the chat-specific 400 param-strip auto-retry and
// SSE transform pipeline.
func (h *Handler) attemptPassthroughCandidate(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome {
	logData := st.logData
	logData.providerID = candidate.provider.ID
	logData.providerName = candidate.provider.Name
	if st.isFailover {
		logData.resolvedModelID = candidate.model.ModelID
	}
	if attempt == 0 {
		debuglog.Info("proxy: routing to provider", "endpoint", logData.endpointType, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "total_candidates", totalCandidates)
	} else {
		debuglog.Info("proxy: failover attempt", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID)
	}
	h.touchProviderLastUsed(candidate.provider.ID)

	// Per-attempt DNS resolution timing, written by SafeDialer via context.
	var dialMs float64
	failoverCtx, failoverCancel := context.WithTimeout(r.Context(), st.failoverTimeout)
	// Own the request context: fires on every return path, after the
	// pass-through dispatch below has consumed the body.
	defer failoverCancel()
	failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")

	proxyReq, _, _, err := h.buildCandidateRequest(failoverCtx, st, candidate)
	if err != nil {
		st.lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
		return outcomeFailover
	}

	resp, ok := h.doUpstream(failoverCtx, proxyReq, st, candidate, attempt, &dialMs)
	if !ok {
		return outcomeFailover
	}

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0
	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	if isFailoverEligible {
		h.recordBreakerOutcome(st, candidate, resp.StatusCode, true)
		if hasMoreCandidates {
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			st.lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
			debuglog.Info("proxy: failover triggered", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
			logData.failoverAttempt = attempt
			return outcomeFailover
		}
	} else if st.circuitBreakerEnabled {
		// No TTFT probe on pass-through responses: a non-failover-eligible
		// status means the provider answered, so the success is recorded here
		// (chat defers a streaming 200 to the probe; multimodal does not).
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, hasMoreCandidates, responseHeaderMs)
	}

	debuglog.Debug("proxy: upstream responded OK, dispatching passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
	h.servePassthroughResponse(w, r, st, resp, attempt, responseHeaderMs)
	return outcomeServed
}

// servePassthroughResponse forwards a successful upstream response to the
// client verbatim. Three response shapes:
//   - application/json: buffered copy-through with token-usage extraction
//     (only usage counts are read; content is never inspected or logged)
//   - text/event-stream: flush-per-read streaming copy (image partial_images,
//     TTS stream_format=sse, STT stream=true)
//   - anything else (binary audio, images): streaming copy
func (h *Handler) servePassthroughResponse(w http.ResponseWriter, r *http.Request, st *requestState, resp *http.Response, attempt int, responseHeaderMs float64) {
	defer func() {
		// Drain remaining bytes so the Transport reuses the connection,
		// unless the client already disconnected.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	logData := st.logData

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}

	isSSE := strings.HasPrefix(contentType, "text/event-stream")
	isJSON := !isSSE && strings.Contains(contentType, "json")

	if isJSON {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			debuglog.Warn("proxy: passthrough body read failed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "error", err)
			h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "failed", fmt.Sprintf("upstream body read error: %v", err))
			writeOpenAIError(w, "failed to read upstream response", http.StatusBadGateway)
			return
		}
		promptTokens, completionTokens := extractPassthroughUsage(body)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		if _, writeErr := w.Write(body); writeErr != nil {
			debuglog.Warn("proxy: client write failed during passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "error", writeErr)
		}
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, promptTokens, completionTokens, "completed", "")
		if st.vkHash != "" && (promptTokens > 0 || completionTokens > 0) {
			h.recordTokenUsage(st.vkHash, promptTokens, completionTokens, 0, logData.virtualKeyName)
		}
		debuglog.Info("proxy: passthrough completed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", len(body), "prompt_tokens", promptTokens, "completion_tokens", completionTokens)
		return
	}

	if isSSE {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
	} else if cl := resp.Header.Get("Content-Length"); cl != "" {
		// Binary responses with a known length: pass it through so clients
		// can report download progress.
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(resp.StatusCode)

	written, copyErr := io.Copy(newFlushWriter(w), resp.Body)
	if copyErr != nil {
		errMsg := fmt.Sprintf("response copy error: %v", copyErr)
		if r.Context().Err() != nil {
			errMsg = "client disconnected during response"
		}
		debuglog.Warn("proxy: passthrough copy interrupted", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "bytes", written, "error", copyErr)
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "failed", errMsg)
		return
	}
	h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "completed", "")
	debuglog.Info("proxy: passthrough completed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", written, "sse", isSSE)
}

// finalizePassthroughLog writes the terminal request-log update for a
// multimodal request (the pass-through counterpart of the chat handlers'
// inline logData population).
func (h *Handler) finalizePassthroughLog(st *requestState, statusCode, attempt int, responseHeaderMs float64, promptTokens, completionTokens int, state, errMsg string) {
	logData := st.logData
	logData.statusCode = statusCode
	logData.durationMs = float64(time.Since(st.startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = st.proxyOverhead
	logData.parseMs = st.parseMs
	logData.failoverLookupMs = st.timings.failoverLookupMs
	logData.modelLookupMs = st.timings.modelLookupMs
	logData.providerLookupMs = st.timings.providerLookupMs
	logData.keyDecryptMs = st.timings.keyDecryptMs
	logData.dialMs = st.timings.dialMs
	logData.settingsReadMs = st.timings.settingsReadMs
	logData.responseHeaderMs = responseHeaderMs
	logData.tokensPrompt = promptTokens
	logData.tokensCompletion = completionTokens
	logData.failoverAttempt = attempt
	logData.errorMessage = errMsg
	logData.state = state
	// Fire-and-forget: skip WaitForInsert to avoid blocking the response path
	// (mirrors handleNonStreamingResponse).
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
}

// extractPassthroughUsage reads token counts from a multimodal JSON response.
// Embeddings use prompt_tokens/total_tokens; the images and audio APIs use
// input_tokens/output_tokens. Only the usage object is decoded; the response
// content itself is never inspected.
func extractPassthroughUsage(body []byte) (promptTokens, completionTokens int) {
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Usage == nil {
		return 0, 0
	}
	promptTokens = resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = resp.Usage.InputTokens
	}
	completionTokens = resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = resp.Usage.OutputTokens
	}
	return promptTokens, completionTokens
}

// flushWriter flushes the underlying ResponseWriter after every write so
// streamed pass-through bytes (SSE events, audio chunks) reach the client
// immediately instead of sitting in the server's buffer.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func newFlushWriter(w http.ResponseWriter) flushWriter {
	f, _ := w.(http.Flusher)
	return flushWriter{w: w, f: f}
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}
