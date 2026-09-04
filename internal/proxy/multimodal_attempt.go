package proxy

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// attemptPassthroughCandidate runs one failover attempt for a multimodal
// request: build and send the upstream request, record the circuit-breaker
// outcome, and either fail over to the next candidate, forward a terminal
// error, or stream the response through.
func (h *Handler) attemptPassthroughCandidate(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome {
	logData := st.logData
	// Per-attempt DNS resolution timing, written by SafeDialer via context.
	var dialMs float64
	failoverCtx, failoverCancel := context.WithTimeout(r.Context(), st.failoverTimeout)
	// Fires on every return path, after the pass-through dispatch has
	// consumed the body.
	defer failoverCancel()
	failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")

	// A Gemini TTS or STT candidate that cannot serve the request as asked
	// (a format it does not produce, an upload it cannot read) is skipped
	// before any request is built, so another candidate in the group can
	// serve it. Nothing was contacted, so the skip is recorded on the trail
	// like a breaker skip and pays no failover backoff.
	if reason := geminiRequestRefusal(st, candidate); reason != "" {
		st.setReqErr(reqError{Kind: KindProviderBadRequest, Attempt: attempt, Provider: candidate.provider.Name, Underlying: reason})
		logData.failoverAttempt = attempt
		logData.appendSkip(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), reason)
		debuglog.Info("proxy: gemini candidate skipped", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "reason", reason)
		return outcomeSkipped
	}

	resp, providerType, _, busyAttempt, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if busyAttempt {
		return outcomeBusy
	}
	if !ok {
		return outcomeFailover
	}

	// MiniMax reports business errors (rate limit, exhausted plan balance, auth
	// failures) inside an HTTP 200 envelope, so the status is normalised before
	// anything is judged from it.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)
	h.finishAttemptAdmission(st, candidate, resp)
	logData.noteAttemptStatus(resp.StatusCode)

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0
	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	rl := h.judge429AndRecordBreaker(r.Context(), st, candidate, resp, isFailoverEligible)

	if isFailoverEligible {
		if hasMoreCandidates {
			// The body is discarded anyway, so classify it on the way out: a
			// retired model usually answers 404, which is failover-eligible, so
			// without this the "model gone" signal is lost whenever there is
			// another candidate to fall back to. The read is capped because on
			// multimodal endpoints the body behind an error status can be an
			// image payload rather than a sentence.
			drained, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			drainedMsg := util.SanitizeLogBody(string(drained), 10000)
			kind, _ := classifyUpstreamError(resp.StatusCode, drainedMsg, candidate.model.ModelID)
			if kind == KindProviderModelGone {
				h.noteModelGone(candidate, logData.endpointType)
			}
			st.setReqErr(failoverReqErr(rl, attempt, candidate.provider.Name, resp.StatusCode))
			debuglog.Info("proxy: failover triggered", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode, "rate_limit_class", rl.class.String())
			logData.failoverAttempt = attempt
			logData.closeAttemptRecord(resp.StatusCode, st.lastReqErr.Kind, drainedMsg, rl.phrase, 0)
			return outcomeFailover
		}
		// The last candidate's one-shot retries, the same as the chat path.
		if outcome, ok := h.deferLastCandidateRetry(st, candidate, resp, attempt, rl); ok {
			return outcome
		}
	}

	if !servedSuccessStatus(resp.StatusCode) {
		// A non-failover-eligible error (e.g. 400) means the provider is alive:
		// credit the circuit before forwarding. RecordAlive, not RecordSuccess:
		// nothing was served, so the 429 behavioural fallback must not count it
		// as a recent serve.
		if !isFailoverEligible && st.circuitBreakerEnabled {
			logData.noteBreaker(breakerAlive)
			h.circuitBreaker.RecordAlive(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), resp.StatusCode)
		}
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, isFailoverEligible, responseHeaderMs)
	}

	// Breaker success for 2xx and the gone-strike clear both happen inside
	// servePassthroughResponse at the commit point (the buffered read for
	// JSON, the first body byte for SSE/binary), so a provider that returns
	// 200 headers and then stalls before producing data still accrues breaker
	// failures.
	debuglog.Debug("proxy: upstream responded OK, dispatching passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
	if st.speechFormat != "" {
		return h.serveGeminiSpeechResponse(w, r, st, candidate, resp, attempt, responseHeaderMs)
	}
	if st.transcriptionFormat != "" {
		return h.serveGeminiTranscriptionResponse(w, r, st, candidate, resp, attempt, responseHeaderMs)
	}
	h.servePassthroughResponse(w, r, st, candidate, resp, attempt, responseHeaderMs)
	return outcomeServed
}

// passthroughAnswered reports whether a buffered pass-through response is the
// model answering, for the purpose of clearing its gone-strike streak.
//
// Embeddings is judged on content, since it is the only pass-through family
// that can be auto-retired: a provider alternating gone-shaped 404s with
// 200 {"data":[]} would otherwise reset the count on every empty answer.
// Everything else is judged on bytes, because image and audio answers are
// forwarded verbatim and are not parsed here.
func passthroughAnswered(endpointType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	// Past the buffer cap, body is a prefix: the caller read
	// passthroughJSONBufferCap+1 bytes and streams the remainder. Truncated
	// JSON never parses, so the content check below would report a provider
	// that produced megabytes as having answered with nothing.
	if len(body) > passthroughJSONBufferCap {
		return true
	}
	if endpointType == endpointTypeEmbeddings {
		return probeDeliveredContent(endpointTypeEmbeddings, body)
	}
	return true
}
