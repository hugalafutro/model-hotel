package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
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
	// ingestRequest sizes the prompt with the chat rule, which finds no
	// "messages" in these bodies and so returns zero for every one of them.
	// Size them by their own shape instead, or the metering estimate below is
	// silently no charge at all.
	st.logData.promptTextBytes = passthroughPromptTextBytes(st.bodyBytes, endpointType)
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
			var raw map[string]any
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

	resp, providerType, _, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if !ok {
		return outcomeFailover
	}

	// MiniMax reports business errors (rate limit, exhausted plan balance, auth
	// failures) inside an HTTP 200 envelope, so the status has to be normalised
	// before anything is judged from it — as attemptCandidate,
	// probeStreamingCandidate and probeModel all do. This loop was the one that
	// did not, and the 2xx branch below now clears the model's gone-strike
	// streak, so a refusal wrapped in a 200 was recorded as the model answering.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0
	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	if isFailoverEligible {
		h.recordBreakerOutcome(st, candidate, resp.StatusCode, true)
		if hasMoreCandidates {
			// The body is discarded anyway, so classify it on the way out — what
			// attemptCandidate does for chat, and for the same reason: a retired
			// model usually answers 404, which is failover-eligible, so without
			// this the "model gone" signal is lost precisely when there is another
			// candidate to fall back to. Only the LAST candidate in a group
			// reached forwardUpstreamError and struck, so an embeddings model
			// anywhere else in a multi-candidate group accrued no strikes at all.
			//
			// Bounded, and the cap matters more here than on the chat path that
			// shares it: these are the multimodal endpoints, where the body behind
			// an error status can be an image payload rather than a sentence.
			drained, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if kind, _ := classifyUpstreamError(resp.StatusCode, util.SanitizeLogBody(string(drained), 10000), candidate.model.ModelID); kind == KindProviderModelGone {
				h.noteModelGone(candidate, logData.endpointType)
			}
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
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, isFailoverEligible, responseHeaderMs)
	}

	// Breaker success for 2xx is recorded inside servePassthroughResponse at
	// the commit point (the buffered read for JSON, the first body byte for
	// SSE/binary), so a provider that returns 200 and then stalls or dies
	// before producing any data still accrues breaker failures. The
	// gone-strike streak is cleared at those same two points and for the same
	// reason: 200 headers are a promise, not evidence.
	debuglog.Debug("proxy: upstream responded OK, dispatching passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
	h.servePassthroughResponse(w, r, st, candidate, resp, attempt, responseHeaderMs)
	return outcomeServed
}

// passthroughAnswered reports whether a buffered pass-through response is the
// model answering, for the purpose of clearing its gone-strike streak.
//
// Embeddings is judged on content, by the same function the probe uses. It is
// the only pass-through family that can be auto-retired, so it is the only one
// where getting this wrong has a consequence: a provider alternating gone-shaped
// 404s with 200 {"data":[]} would otherwise reset the count on every empty
// answer and the model would never be nominated. Same failure
// chatAnswerCarriesContent closes on the chat path.
//
// Everything else is judged on bytes, deliberately: this path forwards image and
// audio answers verbatim and has no business parsing them. Being generous there
// costs nothing, because noteModelServed clears the streak for the surface the
// response arrived on and those surfaces have no streak to clear.
func passthroughAnswered(endpointType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	// Past the buffer cap, body is a PREFIX: the caller read
	// passthroughJSONBufferCap+1 bytes and streams the remainder. Truncated JSON
	// never parses, so handing it to the content check below would report that a
	// provider which just produced megabytes had answered with nothing — and a
	// batch embeddings call clears 8 MiB at around 140 inputs of 3072 dimensions,
	// which is ordinary document-indexing traffic.
	if len(body) > passthroughJSONBufferCap {
		return true
	}
	if endpointType == endpointTypeEmbeddings {
		return probeDeliveredContent(endpointTypeEmbeddings, body)
	}
	return true
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
	// An embeddings answer is JSON by definition, so it takes the buffered branch
	// whatever an aggregator or CDN in front of the provider labelled it. Letting
	// the content type decide sent an unlabelled one to the streamed twin, which
	// commits on the first byte and cannot judge what it never holds — so
	// `{"data":[]}` is eleven bytes and clears the streak, routing around the
	// check passthroughAnswered exists to make. Embeddings is the only
	// pass-through family that can be auto-retired, hence the only one named.
	isJSON := !isSSE && (strings.Contains(contentType, "json") || st.logData.endpointType == endpointTypeEmbeddings)

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
	// The commit point is where the model has proved it is alive, so it is where
	// its gone-strike streak stops being current. Without it "three CONSECUTIVE
	// refusals" is not true on this path: embeddings strikes would only expire
	// with goneStrikeWindow, so three refusals scattered across half an hour of
	// otherwise healthy traffic would reach the threshold and spend a probe.
	//
	// Here rather than on the 200 headers, because the read above is what
	// distinguishes a provider that served the model from one that promised to: a
	// body that died mid-read is the failure branch, and clearing the streak
	// there would put a model that never actually answers out of reach of a
	// retirement forever.
	//
	// Not gated on circuitBreakerEnabled, unlike its neighbour: the breaker is an
	// operator's routing choice, and whether a model still exists is not.
	//
	// Family-gated inside noteModelServed, exactly as the strike is, so an
	// embeddings 200 says nothing about the chat surface and an image or TTS 200
	// says nothing about either. And gated on bytes having arrived, judged by the
	// same rule the probe uses — see passthroughAnswered.
	answered := passthroughAnswered(logData.endpointType, body)
	if answered {
		h.noteModelServed(candidate.model, logData.endpointType)
	}
	// Oversized is judged on the bytes read, before masking can shrink a
	// cap+1 read under the cap and drop the streamed remainder.
	oversized := len(body) > passthroughJSONBufferCap
	// Exact-key scrub only: a success body is content, where the key-shape
	// regex could false-positive.
	body = logData.masker.maskExact(body)
	copyPassthroughHeaders(w, resp, contentType)

	if oversized {
		// Oversized JSON (e.g. several b64 images): forward the buffered
		// prefix and stream the rest; usage extraction is skipped to keep
		// memory bounded.
		// The remainder streams through exactMaskWriter so a key straddling
		// the buffered-prefix boundary, or any two reads, is still masked.
		w.WriteHeader(resp.StatusCode)
		written := int64(len(body))
		ew := newExactMaskWriter(w, logData.masker)
		if _, writeErr := ew.Write(body); writeErr == nil {
			n, _ := io.Copy(ew, resp.Body)
			written += n
			_ = ew.Flush()
		}
		// Skipping usage EXTRACTION must not mean skipping metering: the
		// provider billed for this request and the client got the whole
		// response, so recording (0,0) here charged it against nothing — not
		// the key's tokens_used counter, not its TPM budget. The cap is 8 MiB
		// and a batch embeddings call clears it at around 140 inputs, so the
		// free requests were the routine ones, not the exotic ones.
		//
		// Only the prompt is estimated. The streaming path derives output from
		// delivered bytes because those bytes are text; here they are float
		// vectors or base64 image data, so the same arithmetic would invent
		// roughly two million completion tokens for one 8 MiB embeddings
		// response. Undercharging the output is the deliberate choice, and it
		// is still strictly better than charging nothing.
		// The log keeps the provider's figures, which here are none: usage was
		// never extracted, so nothing was measured. The estimate charges the
		// quota WITHOUT being reported as measured usage, matching what the
		// chat and streaming paths do (they update the log before calling
		// estimateMissingUsage) and keeping the stats pages honest.
		//
		// Charged through the same helper as every other pass-through branch,
		// rather than hand-rolled here. Hand-rolling it is what left this branch
		// free: it carried its own `if estimated > 0` guard, so the zero-prompt
		// families skipped the debit exactly as they did below. /images/variations
		// has no prompt field at all, and four b64_json images clear the 8 MiB cap
		// routinely, so the endpoint that motivated the floor was still free on
		// precisely the requests the provider bills most for.
		h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, 0, 0, "completed", "")
		estimated, _ := h.chargePassthroughUsage(st, 0, 0, answered)
		debuglog.Info("proxy: passthrough completed (oversized json)", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", written, "estimated_prompt_tokens", estimated)
		return
	}

	promptTokens, completionTokens := extractPassthroughUsage(body)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode)
	//nolint:gosec // G705 false positive: provider JSON body, not HTML; Content-Type is application/json
	if _, writeErr := w.Write(body); writeErr != nil {
		debuglog.Warn("proxy: client write failed during passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "error", writeErr)
	}
	// The log records what the provider measured, which may be nothing.
	h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, promptTokens, completionTokens, "completed", "")

	// Charge the quota even when the provider reported no usage at all. Image
	// generation and text-to-speech routinely omit the usage block, so guarding
	// the debit on "did it report something" left those families unmetered on
	// every ordinary request, not merely on the oversized ones the sibling
	// branch above handles. Reported figures always win; the estimate only
	// fills a total absence, and only for the prompt, for the same reason the
	// oversized branch does not size a response of vectors or base64 as text.
	charged, estimatedPrompt := h.chargePassthroughUsage(st, promptTokens, completionTokens, answered)
	debuglog.Info("proxy: passthrough completed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", len(body), "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "charged_tokens", charged, "prompt_estimated", estimatedPrompt)
}

// chargePassthroughUsage debits a completed pass-through request, estimating
// the prompt when the provider reported no usage at all.
//
// Every pass-through family needs this and they do not share a branch: JSON
// bodies are metered in serveBufferedJSONPassthrough, while audio/mpeg and
// other binary shapes go to serveStreamedPassthrough, where the SSE tail that
// carries usage is not even allocated. Guarding the debit on "did the provider
// report something" therefore left image generation unmetered on the JSON side
// and text-to-speech unmetered on the binary side, on every ordinary request.
//
// delivered gates the estimate, not the reported figures: a provider that
// answers 200 with nothing (an aggregator in front of a retired model returning
// `{"data":[]}`) has cost nothing, and charging a full prompt estimate for it
// would bill the caller for every empty answer. This is the same rule
// estimateMissingUsage states for streams: nothing is estimated when no output
// was delivered.
//
// Only the prompt is ever estimated. A pass-through response body is float
// vectors or base64 or audio, not text, so sizing it the way the streaming path
// sizes delivered text would invent an enormous completion charge.
//
// A delivered request that reaches this helper is never charged zero. (Every
// pass-through branch does reach it -- the oversized-JSON one was hand-rolling
// its own debit and was the last exception.) The estimate legitimately sizes to
// nothing on the multipart families: multipartPromptTextBytes counts only the
// "prompt" form field, which is optional on transcriptions and translations and
// absent entirely from /images/variations, so the ordinary shape of those
// requests carries no promptable text at all. Without the floor a served
// transcription cost the key nothing — no tokens_used, no TPM draw — on every
// request. The upload is still not measured, for the reason given on
// multipartPromptTextBytes, so this is deliberately a floor and not a
// proportional estimate: it makes the request countable, not priced. A provider
// that reports real usage always displaces it, and per-key RPS limiting -- on by
// default over the whole /v1 group, though an operator can set rps <= 0 for
// unlimited -- bounds request volume independently.
//
// Returns what was charged and whether the prompt was estimated, for the log.
func (h *Handler) chargePassthroughUsage(st *requestState, promptTokens, completionTokens int, delivered bool) (charged int, estimated bool) {
	logData := st.logData
	chargePrompt, chargeCompletion := promptTokens, completionTokens
	if chargePrompt == 0 && chargeCompletion == 0 {
		if !delivered {
			return 0, false
		}
		chargePrompt, estimated = estimateTokens(logData.promptTextBytes), true
		if chargePrompt < minPassthroughTokens {
			chargePrompt = minPassthroughTokens
		}
	}
	if chargePrompt > 0 || chargeCompletion > 0 {
		h.recordTokenUsage(st.vkHash, logData, chargePrompt, chargeCompletion, 0)
	}
	return chargePrompt + chargeCompletion, estimated
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
	// The streamed commit point, matching the buffered one: a first byte out of
	// the provider is where a 200 stops being a promise. See the twin call in
	// serveBufferedJSONPassthrough.
	//
	// Gated on a byte having arrived, which the breaker call above deliberately
	// is not: they answer different questions. A 204 carrying nothing is a
	// legitimate HTTP success and the provider is plainly alive, so it belongs in
	// the breaker's ledger, but it says nothing about whether this MODEL is still
	// served — the same rule judgeProbeSuccess applies to the probe's own 200s.
	if n > 0 {
		h.noteModelServed(candidate.model, logData.endpointType)
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
	var masker *sseErrorMaskWriter
	if isSSE {
		tail = newTailBuffer(passthroughSSETailCap)
		masker = newSSEErrorMaskWriter(io.MultiWriter(newFlushWriter(w), tail), logData.masker)
		dst = masker
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
	if masker != nil {
		// Release a trailing unterminated line (a stream cut mid-event) so the
		// client receives every byte the provider sent, masked or not.
		if err := masker.Flush(); err != nil && copyErr == nil {
			copyErr = err
		}
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
		// The provider billed whatever it produced, whether or not the client
		// stayed to receive it. Bytes reached the client, so an absent usage
		// report is estimated rather than treated as free: this is the path
		// audio/mpeg takes, where the SSE tail that would carry usage is never
		// even allocated, so the report is structurally always absent.
		h.chargePassthroughUsage(st, promptTokens, completionTokens, written > 0)
		return
	}
	h.finalizePassthroughLog(st, resp.StatusCode, attempt, responseHeaderMs, promptTokens, completionTokens, "completed", "")
	charged, estimatedPrompt := h.chargePassthroughUsage(st, promptTokens, completionTokens, written > 0)
	debuglog.Info("proxy: passthrough completed", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "bytes", written, "sse", isSSE, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "charged_tokens", charged, "prompt_estimated", estimatedPrompt)
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
// The usage member is lifted out before its counts are read, for two reasons.
// util.DecodeCounts re-parses the document it is given on each coercion pass,
// and an embeddings body is large; and reading only those bytes is what makes
// the sentence above literally true rather than merely intended.
func extractPassthroughUsage(body []byte) (promptTokens, completionTokens int) {
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Usage) == 0 {
		return 0, 0
	}
	var usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}
	// util.DecodeCounts, and a shape error keeps what did read, for the reason
	// given on Usage.UnmarshalJSON: a count quoted or written with a fraction on
	// it is still a count, and one member this gateway cannot read must not cost
	// the request every count beside it. Metering a served request as zero is a
	// quota the caller never spends.
	if err := util.DecodeCounts(envelope.Usage, &usage); err != nil && shapeError(envelope.Usage, err) == nil {
		return 0, 0
	}
	promptTokens = usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = usage.InputTokens
	}
	if promptTokens == 0 {
		promptTokens = usage.TotalTokens
	}
	completionTokens = usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = usage.OutputTokens
	}
	return promptTokens, completionTokens
}

// extractPassthroughSSEUsage scrapes token counts from the trailing bytes of
// a pass-through SSE stream: OpenAI streaming responses carry usage on the
// final (small) event, so scanning the retained tail is enough. A leading
// partial line (cut by the tail cap) simply fails to parse and is skipped.
func extractPassthroughSSEUsage(tail []byte) (promptTokens, completionTokens int) {
	for line := range strings.SplitSeq(string(tail), "\n") {
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

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}
