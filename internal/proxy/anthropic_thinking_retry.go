package proxy

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The Messages-route self-heal, sibling of the chat-completions param retry. It
// covers the two 400s that route can fix by asking differently, both of which
// are per-model facts that no model id reveals:
//
//   - The extended-thinking shape. A model takes adaptive, budget, or both
//     (anthropicegress.ThinkingDialect); the proxy asks in its best-known shape
//     and switches on the 400 that names the other.
//   - A param the model has retired. Anthropic deprecates sampling params per
//     model generation, so claude-sonnet-5 and claude-opus-5 answer
//     "`temperature` is deprecated for this model" where every 4.x model accepts
//     it, and OpenAI clients send temperature as a matter of course.
//
// Either way the fact is learned for this provider and model and the request
// re-issued once, so the caller sees an answer rather than a 400 and no later
// request to that model pays the round trip again.
//
// One retry is enough for both, because they cannot co-occur: a request that
// asks for thinking has its sampling params dropped in translation (Anthropic
// rejects them alongside thinking), and a request that does not ask for thinking
// cannot earn a dialect complaint.
//
// This cannot ride on retryWithStrippedParams, which is skipped for every
// dialect attempt (sentChatCompletionsBody) because it rebuilds an OpenAI body
// and re-POSTs it, and /v1/messages would reject that.

// retryLearnableMessages400 handles a 400 from an Anthropic egress attempt. It
// returns handled=false for any 400 it cannot learn from, leaving the response
// untouched for the caller to fail over or forward as it would have.
//
// The contract matches retryWithResponses: on handled=true the caller adopts
// res.resp (and res.retryCancel, whose body it must consume), or res.cont with
// res.lastReqErr when the retry could not be issued.
func (h *Handler) retryLearnableMessages400(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType string,
	resp *http.Response,
	attempt int,
	dialMs *float64,
	failoverCancel context.CancelFunc,
	streamCancelOrigin string,
) (paramRetryResult, bool) {
	res := paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}
	if !st.anthropicEgressAttempt || st.messagesRetried {
		return res, false
	}

	// The same bounded read the other learners use: the body is decoded here and
	// still has to be handed on readable when nothing can be learned from it.
	body, readErr := readLearnable400(resp)
	// A no-op close on the buffered replacement, for bodyclose's benefit.
	_ = resp.Body.Close()
	if readErr != nil {
		return res, false
	}

	rebuilt, model, stream, ok := h.learnAndRebuildMessages400(st, candidate, providerType, body)
	if !ok {
		return res, false
	}
	if !st.retryBudgetLeft() {
		// Learned for the next request; this one carries the 400 on as it
		// came (the body is restored) rather than a retry that would time
		// out on issue.
		return res, true
	}

	failoverCancel() // 400 body fully consumed, original context no longer needed

	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/messages")
	retryCtx, rc := retryContext(r, st)
	retryCtx, retryDial := withDialTiming(retryCtx)
	res.streamCancelOrigin = "retry_timeout"

	retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
	if retryErr != nil {
		rc()
		res.lastReqErr = reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
		res.cont = true
		return res, true
	}
	util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
	retryReq.Header.Set("Content-Type", "application/json")

	var checkRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		checkRedirect = h.safeDialer.CheckRedirect
	}
	//nolint:bodyclose // retry resp.Body is consumed by the caller's dispatch
	retryResp, doErr := (&http.Client{Transport: h.upstreamTransport, CheckRedirect: checkRedirect}).Do(retryReq)
	*dialMs += retryDial.take()
	if doErr != nil {
		rc()
		debuglog.Warn("proxy: anthropic thinking dialect retry failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", doErr)
		if errors.Is(doErr, context.Canceled) || errors.Is(doErr, context.DeadlineExceeded) {
			origin := "retry_timeout"
			if errors.Is(doErr, context.Canceled) {
				origin = "client_disconnect"
			}
			res.lastReqErr = reqError{Kind: cancelOriginToKind(origin), Attempt: attempt, Provider: candidate.provider.Name}
		} else {
			res.lastReqErr = reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(doErr)}
		}
		res.cont = true
		return res, true
	}

	// The retry is still an egress attempt, so the response side keeps
	// translating it. The guard flag stops a second round: what this self-heal
	// can learn it has learned, and a 400 after the re-issue is about something
	// else.
	st.messagesRetried = true
	st.lastMessagesBody = rebuilt
	res.resp = retryResp
	res.retryCancel = rc
	res.retried = true
	debuglog.Info("proxy: anthropic messages self-heal retry issued", "model", model, "stream", stream, "status", retryResp.StatusCode)
	return res, true
}

// learnAndRebuildMessages400 reads what a Messages 400 has to teach, records it,
// and rebuilds the request accordingly. ok is false when the body teaches
// nothing, or when what it teaches cannot change this particular request, where
// re-issuing would send identical bytes and earn the identical 400.
func (h *Handler) learnAndRebuildMessages400(st *requestState, candidate modelCandidate, providerType string, body []byte) (rebuilt []byte, model string, stream, ok bool) {
	if dialect, isDialectError := anthropicegress.DialectFromError(body); isDialectError {
		// Learn before deciding whether this request can be retried: a hedged
		// attempt reaches the same learner, and the cached fact stops the next
		// request repeating the mistake even when this one cannot be re-issued.
		h.learnThinkingDialect(candidate, dialect)
		rebuilt, model, stream, err := h.anthropicEgressBody(st, candidate, providerType, dialect)
		if err != nil {
			debuglog.Warn("proxy: anthropic messages retry could not rebuild for dialect", "provider", candidate.provider.Name, "model", candidate.model.ModelID, "error", err)
			return nil, "", false, false
		}
		// Only a request that actually asked for thinking is changed by asking in
		// the other dialect.
		if !anthropicegress.RequestAsksForThinking(rebuilt) {
			return nil, "", false, false
		}
		return rebuilt, model, stream, true
	}

	// Param learning is scoped to anthropic-messages, whose ONLY route is
	// Messages. The learned strip is keyed by provider and model and consulted
	// by every build for that key, so learning one on an `anthropic` provider,
	// whose default route is the OpenAI-compat endpoint, would let a name read
	// out of a Messages 400 strip a param from compat traffic that accepts it.
	if providerType != "anthropic-messages" {
		return nil, "", false, false
	}
	rejected := learnableRejections(st, body)
	if len(rejected) == 0 {
		return nil, "", false, false
	}
	// The names shared by the two dialects are the ones the translator forwards
	// unchanged (temperature, top_p, top_k), so a name learned here means the
	// same thing it would on the compat path.
	h.learnRejectedParams(st, candidate, body)
	rebuilt, model, stream, err := h.anthropicEgressBody(st, candidate, providerType, h.thinkingDialectFor(candidate))
	if err != nil {
		debuglog.Warn("proxy: anthropic messages retry could not rebuild without rejected params", "provider", candidate.provider.Name, "model", candidate.model.ModelID, "error", err)
		return nil, "", false, false
	}
	// The rebuild reads the cache just written, so a param that survived it was
	// not actually removed and the retry would be pointless.
	if bytes.Equal(rebuilt, st.lastMessagesBody) {
		return nil, "", false, false
	}
	return rebuilt, model, stream, true
}
