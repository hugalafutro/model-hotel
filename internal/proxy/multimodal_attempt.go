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

// The multimodal pass-through's per-candidate attempt, split out of
// multimodal.go when that file hit the size ceiling. The serve pipeline that
// dispatches a successful attempt stays there.

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

	resp, providerType, _, busyAttempt, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if busyAttempt {
		return outcomeBusy
	}
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

	rl := h.judge429AndRecordBreaker(r.Context(), st, candidate, resp, isFailoverEligible)

	if isFailoverEligible {
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
			drainedMsg := util.SanitizeLogBody(string(drained), 10000)
			kind, _ := classifyUpstreamError(resp.StatusCode, drainedMsg, candidate.model.ModelID)
			if kind == KindProviderModelGone {
				h.noteModelGone(candidate, logData.endpointType)
			}
			st.setReqErr(failoverReqErr(rl, attempt, candidate.provider.Name, resp.StatusCode))
			debuglog.Info("proxy: failover triggered", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode, "rate_limit_class", rl.class.String())
			logData.failoverAttempt = attempt
			return outcomeFailover
		}
		// A saturated 429 on the LAST candidate: wait the seconds the provider
		// asked for and retry it once, exactly as the chat loop does.
		if rl.class == rateLimitSaturated && !st.saturationRetried {
			return h.deferSaturatedRetry(st, candidate, resp, attempt)
		}
	}

	if !servedSuccessStatus(resp.StatusCode) {
		// A definitive non-failover-eligible error (e.g. 400) means the
		// provider is alive: credit the circuit before forwarding, matching
		// chat's recordBreakerOutcome for non-eligible statuses. RecordAlive,
		// not RecordSuccess: nothing was served, so the 429 behavioural
		// fallback must not count it as a recent serve.
		if !isFailoverEligible && st.circuitBreakerEnabled {
			h.circuitBreaker.RecordAlive(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate))
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
