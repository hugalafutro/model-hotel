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

// Rerank proxies POST /v1/rerank requests (Cohere-style rerank API, the
// de-facto standard shape also served by Jina, Voyage, and TEI). The body
// (query + documents) passes through verbatim; Cohere-family providers are
// routed to the native /v2/rerank endpoint since rerank is not part of
// their OpenAI-compatibility surface.
func (h *Handler) Rerank(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/rerank", endpointTypeRerank)
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
	st.longRunning = isLongRunningEndpoint(endpointType)
	st.makeUpstreamBody = makeJSONModelRewriter(st.bodyBytes, st.reqModel)
	h.servePassthroughPipeline(w, r, st)
}

// isLongRunningEndpoint reports whether an endpoint family's legitimate
// latency rivals streaming chat: image generation and audio synthesis/
// transcription regularly take minutes. Embeddings respond in seconds and
// keep the standard budget.
func isLongRunningEndpoint(endpointType string) bool {
	switch endpointType {
	case endpointTypeImage, endpointTypeTTS, endpointTypeSTT:
		return true
	default:
		return false
	}
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
	st.longRunning = isLongRunningEndpoint(endpointType)
	st.makeUpstreamBody = newMultipartBodyBuilder(parts)
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
// everything else untouched. Numbers are decoded as json.Number so large
// integers (e.g. 64-bit seeds beyond 2^53) survive the round-trip without
// float64 precision loss. An unparseable body is forwarded as-is, mirroring
// chat's paramrewrite.BuildUpstreamBody behavior.
func makeJSONModelRewriter(body []byte, requestModel string) func(string) ([]byte, string, error) {
	return func(resolvedModelID string) ([]byte, string, error) {
		out := body
		if requestModel != resolvedModelID {
			// Best-effort rewrite: an unparseable body is forwarded as-is
			// (mirrors paramrewrite.BuildUpstreamBody).
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.UseNumber()
			var raw map[string]interface{}
			if dec.Decode(&raw) == nil {
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

// newMultipartBodyBuilder returns a makeUpstreamBody fn that rebuilds the
// multipart form with the resolved model ID, memoizing the last build:
// failover-group candidates frequently resolve to the same upstream model ID
// (the same model offered by different providers), so the expensive full
// re-serialization of the upload happens once per distinct model instead of
// once per attempt.
func newMultipartBodyBuilder(parts []multipartPart) func(string) ([]byte, string, error) {
	var lastModelID, lastContentType string
	var lastBody []byte
	return func(resolvedModelID string) ([]byte, string, error) {
		if lastBody != nil && resolvedModelID == lastModelID {
			return lastBody, lastContentType, nil
		}
		body, contentType, err := rebuildMultipartBody(parts, resolvedModelID)
		if err != nil {
			return nil, "", err
		}
		lastModelID, lastBody, lastContentType = resolvedModelID, body, contentType
		return body, contentType, nil
	}
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

	// Create the log entry early so early-return paths can record failures.
	// modelID gets updated after the multipart form is parsed.
	logData, vkHash := h.newPendingRequestLog(r, endpointType, "", false)

	// Multipart bodies are never buffered by streamingAwareTimeout (the
	// middleware passes them through unread, post-auth memory only), so the
	// body is always read here.
	parseStart := time.Now()
	bodyBytes, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		debuglog.Warn("proxy: failed to read multipart request body", "error", err)
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "failed to read request body", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
		return nil, nil, false
	}

	mediaType, ctParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || ctParams["boundary"] == "" {
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "Content-Type must be multipart/form-data with a boundary", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "Content-Type must be multipart/form-data with a boundary", http.StatusBadRequest)
		return nil, nil, false
	}

	parts, reqModel, err := parseMultipartParts(bodyBytes, ctParams["boundary"])
	if err != nil {
		debuglog.Warn("proxy: failed to parse multipart form", "error", err)
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "invalid multipart form", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "invalid multipart form", http.StatusBadRequest)
		return nil, nil, false
	}
	parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

	logData.modelID = reqModel
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, KindValidation, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, nil, false
	}

	debuglog.Info("proxy: multipart request start", "endpoint", endpointType, "model", reqModel, "key", logData.virtualKeyName, "parts", len(parts), "client_ip", r.RemoteAddr)

	// bodyBytes stays nil: the parsed parts are the upstream-body source for
	// multipart requests (via makeUpstreamBody), so retaining the raw body
	// would pin a redundant full copy of the upload for the request lifetime.
	return &requestState{
		startTime: startTime,
		reqModel:  reqModel,
		vkHash:    vkHash,
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
	// Per-attempt DNS resolution timing, written by SafeDialer via context.
	var dialMs float64
	failoverCtx, failoverCancel := context.WithTimeout(r.Context(), st.failoverTimeout)
	// Own the request context: fires on every return path, after the
	// pass-through dispatch below has consumed the body.
	defer failoverCancel()
	failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")

	resp, _, _, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
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
			st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)})
			debuglog.Info("proxy: failover triggered", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
			logData.failoverAttempt = attempt
			return outcomeFailover
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// A definitive non-failover-eligible error (e.g. 400) means the
		// provider is alive: record the success before forwarding, matching
		// chat's recordBreakerOutcome for non-eligible statuses.
		if !isFailoverEligible && st.circuitBreakerEnabled {
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, hasMoreCandidates, responseHeaderMs)
	}

	// Breaker success for 2xx is recorded inside servePassthroughResponse at
	// the commit point (headers for buffered JSON, first body byte for
	// SSE/binary), so a provider that returns 200 and then stalls or dies
	// before producing any data still accrues breaker failures.
	debuglog.Debug("proxy: upstream responded OK, dispatching passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
	h.servePassthroughResponse(w, r, st, candidate, resp, attempt, responseHeaderMs)
	return outcomeServed
}

// passthroughJSONBufferCap bounds how much of a JSON pass-through response is
// buffered for token-usage extraction. Bodies beyond the cap (e.g. multi-image
// b64_json payloads) are streamed through unbuffered with usage skipped,
// keeping per-request memory bounded.
const passthroughJSONBufferCap = 8 << 20 // 8MB

// passthroughSSETailCap is how many trailing SSE bytes are retained for usage
// extraction: the usage-bearing event is the final (small) event of OpenAI
// streaming responses, after potentially multi-MB partial-image events.
const passthroughSSETailCap = 64 << 10 // 64KB

// servePassthroughResponse forwards a successful (2xx) upstream response to
// the client verbatim. Three response shapes:
//   - application/json: bounded buffered copy-through with token-usage
//     extraction (only usage counts are read; content is never inspected or
//     logged); oversized bodies stream through with usage skipped
//   - text/event-stream: flush-per-read streaming copy (image partial_images,
//     TTS stream_format=sse, STT stream=true) with usage scraped from the
//     trailing events
//   - anything else (binary audio, images): plain streaming copy
//
// Circuit-breaker success is recorded at the commit point: immediately for
// buffered JSON (headers received, body about to be read), and at the first
// body byte for streamed responses, so a provider that returns 200 and then
// produces nothing records a breaker failure instead of a success.
func (h *Handler) servePassthroughResponse(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, responseHeaderMs float64) {
	defer func() {
		// Drain remaining bytes so the Transport reuses the connection,
		// unless the client already disconnected.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	isSSE := strings.HasPrefix(contentType, "text/event-stream")
	isJSON := !isSSE && strings.Contains(contentType, "json")

	if isJSON {
		h.serveBufferedJSONPassthrough(w, st, candidate, resp, contentType, attempt, responseHeaderMs)
		return
	}
	h.serveStreamedPassthrough(w, r, st, candidate, resp, contentType, isSSE, attempt, responseHeaderMs)
}

// serveBufferedJSONPassthrough handles the application/json shape: bounded
// buffering for usage extraction with streamed forwarding beyond the cap.
// The circuit breaker commits only once the buffered read succeeds — a 200
// whose body dies mid-read records a failure, not a success — and response
// headers are only written after that point, so the read-error path emits a
// clean OpenAI error response.
func (h *Handler) serveBufferedJSONPassthrough(w http.ResponseWriter, st *requestState, candidate modelCandidate, resp *http.Response, contentType string, attempt int, responseHeaderMs float64) {
	logData := st.logData

	body, err := io.ReadAll(io.LimitReader(resp.Body, passthroughJSONBufferCap+1))
	if err != nil {
		if st.circuitBreakerEnabled {
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
		}
		debuglog.Warn("proxy: passthrough body read failed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "error", err)
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "failed", fmt.Sprintf("upstream body read error: %v", err))
		writeOpenAIError(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}
	if st.circuitBreakerEnabled {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}
	copyPassthroughHeaders(w, resp, contentType)

	if len(body) > passthroughJSONBufferCap {
		// Oversized JSON (e.g. several b64 images): forward the buffered
		// prefix and stream the rest; usage extraction is skipped to keep
		// memory bounded.
		w.WriteHeader(resp.StatusCode)
		written := int64(len(body))
		if _, writeErr := w.Write(body); writeErr == nil {
			n, _ := io.Copy(w, resp.Body)
			written += n
		}
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "completed", "")
		debuglog.Info("proxy: passthrough completed (oversized json)", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", written)
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
}

// serveStreamedPassthrough handles SSE and binary shapes: probe the first
// body byte before committing (breaker failure on a dead 200, clean 502
// since no headers have been written), then stream through — flushing
// per-write for SSE only — while retaining an SSE tail for usage metering.
func (h *Handler) serveStreamedPassthrough(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, contentType string, isSSE bool, attempt int, responseHeaderMs float64) {
	logData := st.logData

	// Commit-point probe: a 200 whose body errors — or ends — before the
	// first byte is a provider failure, not a success: SSE and binary 200s
	// promise content (audio bytes, events), so an empty body means the
	// provider broke after committing the status. Non-200 2xx statuses
	// (e.g. 204 No Content) legitimately carry empty bodies and pass through.
	firstByte := make([]byte, 1)
	n, readErr := resp.Body.Read(firstByte)
	emptyBodyIsFailure := resp.StatusCode == http.StatusOK || !errors.Is(readErr, io.EOF)
	if n == 0 && readErr != nil && emptyBodyIsFailure {
		if st.circuitBreakerEnabled && r.Context().Err() == nil {
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
		}
		debuglog.Warn("proxy: passthrough first-byte read failed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "error", readErr)
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "failed", fmt.Sprintf("upstream body read error: %v", readErr))
		writeOpenAIError(w, "upstream produced no response data", http.StatusBadGateway)
		return
	}
	if st.circuitBreakerEnabled {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}

	copyPassthroughHeaders(w, resp, contentType)
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

	// SSE needs an immediate flush per write (event latency) and a trailing
	// tail buffer for usage extraction; binary streams use the ResponseWriter's
	// own buffering — per-chunk flushes would just multiply syscalls.
	var tail *tailBuffer
	var dst io.Writer = w
	if isSSE {
		tail = newTailBuffer(passthroughSSETailCap)
		dst = io.MultiWriter(newFlushWriter(w), tail)
	}

	var written int64
	var copyErr error
	if n > 0 {
		var writeErr error
		nw, writeErr := dst.Write(firstByte[:n])
		written += int64(nw)
		copyErr = writeErr
	}
	if copyErr == nil && readErr == nil {
		var nc int64
		nc, copyErr = io.Copy(dst, resp.Body)
		written += nc
	}

	promptTokens, completionTokens := 0, 0
	if tail != nil {
		promptTokens, completionTokens = extractPassthroughSSEUsage(tail.Bytes())
	}

	if copyErr != nil {
		errMsg := fmt.Sprintf("response copy error: %v", copyErr)
		if r.Context().Err() != nil {
			errMsg = "client disconnected during response"
		}
		debuglog.Warn("proxy: passthrough copy interrupted", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "bytes", written, "error", copyErr)
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, promptTokens, completionTokens, "failed", errMsg)
		return
	}
	h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, promptTokens, completionTokens, "completed", "")
	if st.vkHash != "" && (promptTokens > 0 || completionTokens > 0) {
		h.recordTokenUsage(st.vkHash, promptTokens, completionTokens, 0, logData.virtualKeyName)
	}
	debuglog.Info("proxy: passthrough completed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", written, "sse", isSSE, "prompt_tokens", promptTokens, "completion_tokens", completionTokens)
}

// copyPassthroughHeaders sets the upstream Content-Type and (when present)
// Content-Disposition on the response. Called only once the response is
// committed, so error paths never inherit attachment semantics.
func copyPassthroughHeaders(w http.ResponseWriter, resp *http.Response, contentType string) {
	w.Header().Set("Content-Type", contentType)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
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
// input_tokens/output_tokens; rerank providers (Jina, Voyage) report only
// usage.total_tokens, used as a last-resort prompt count (Cohere's native
// rerank bills in search units, not tokens, and meters as zero). Only the
// usage object is decoded; the response content itself is never inspected.
func extractPassthroughUsage(body []byte) (promptTokens, completionTokens int) {
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Usage == nil {
		return 0, 0
	}
	promptTokens = resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = resp.Usage.InputTokens
	}
	if promptTokens == 0 {
		promptTokens = resp.Usage.TotalTokens
	}
	completionTokens = resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = resp.Usage.OutputTokens
	}
	return promptTokens, completionTokens
}

// extractPassthroughSSEUsage scrapes token counts from the trailing bytes of
// a pass-through SSE stream: OpenAI streaming responses carry usage on the
// final (small) event, so scanning the retained tail is enough. A leading
// partial line (cut by the tail cap) simply fails to parse and is skipped.
func extractPassthroughSSEUsage(tail []byte) (promptTokens, completionTokens int) {
	for _, line := range strings.Split(string(tail), "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !ok {
			continue
		}
		if p, c := extractPassthroughUsage([]byte(strings.TrimSpace(payload))); p > 0 || c > 0 {
			promptTokens, completionTokens = p, c
		}
	}
	return promptTokens, completionTokens
}

// tailBuffer is an io.Writer that retains only the last capacity bytes
// written through it, used to scrape usage from the end of SSE streams
// without buffering multi-MB event payloads.
type tailBuffer struct {
	buf      []byte
	capacity int
}

func newTailBuffer(capacity int) *tailBuffer {
	return &tailBuffer{capacity: capacity}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if len(p) >= t.capacity {
		t.buf = append(t.buf[:0], p[len(p)-t.capacity:]...)
		return len(p), nil
	}
	if overflow := len(t.buf) + len(p) - t.capacity; overflow > 0 {
		t.buf = t.buf[:copy(t.buf, t.buf[overflow:])]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

// Bytes returns the retained tail.
func (t *tailBuffer) Bytes() []byte {
	return t.buf
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
