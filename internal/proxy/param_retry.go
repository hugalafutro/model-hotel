package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// responsesLearnBodyCap bounds a 400 that has to be parsed rather than scanned.
// openairesponses.RequiresResponsesAPI json.Unmarshals the whole error
// document, so a body cut off mid-JSON does not parse and the /v1/responses
// requirement is silently not learned. A megabyte is far past any real 400 and
// still bounded. A learnable 404 is read at the classifier's cap instead.
const responsesLearnBodyCap = 1 << 20

// paramRetryResult is the outcome of the 400 param-stripping auto-retry. It
// tells the failover loop how to proceed:
//   - resp: the response to continue handling with, the last retry's response
//     once any retry was issued, otherwise the original 400. Every 400 that
//     leaves here carries a restored body.
//   - retryCancel: the retry context's cancel func, non-nil only when a retry
//     response is live and its body has not yet been consumed. The caller must
//     call it after consuming the body.
//   - streamCancelOrigin: "retry_timeout" once a retry was issued, otherwise
//     the caller's original value, unchanged.
//   - retried: true once a retry request was issued and answered, whatever it
//     answered with. The caller folds the retries' dial time into the running
//     totals.
//   - lastReqErr: set only when cont is true; the structured cause the caller
//     records via st.setReqErr before failing over.
//   - cont: true means the caller should continue to the next candidate (a
//     retry request could not be created or failed).
type paramRetryResult struct {
	resp               *http.Response
	retryCancel        context.CancelFunc
	streamCancelOrigin string
	retried            bool
	lastReqErr         reqError
	cont               bool
}

// learnedScopeFor scopes the learned reject/rename caches to one provider. The
// provider id, not its type: many distinct custom endpoints share the "openai"
// type, and a type-scoped entry would let one of them disable a param for all
// the others serving the same model id.
func learnedScopeFor(candidate modelCandidate) string {
	if candidate.provider == nil {
		return ""
	}
	return candidate.provider.ID.String()
}

// candidateModelID reads a candidate's upstream model id for the breaker key,
// the metrics and the attempt trail. Some
// paths carry only the provider, so the model is never assumed present.
//
// The empty-string fallback is not inert: the breaker keys a circuit by this
// id, so a modelless candidate charges the "" circuit, which counts toward
// the provider-wide span (SpanModels). Every candidate the
// failover loop routes carries a model, so the fallback is logged at error.
func candidateModelID(candidate modelCandidate) string {
	if candidate.model == nil {
		providerName, providerID := "", ""
		if candidate.provider != nil {
			providerName, providerID = candidate.provider.Name, candidate.provider.ID.String()
		}
		debuglog.Error("proxy: candidate carries no model, charging the breaker's empty circuit", "provider", providerName, "provider_id", providerID)
		return ""
	}
	return candidate.model.ModelID
}

// retryLearnable400 picks the self-heal that fits what this attempt actually
// sent, and reports handled=false when none does. Each family may only read the
// dialect it speaks: a 400 names the fields of the request that earned it, so a
// param mislearned from a Messages 400 would poison the compat path for that
// model on every later request.
//
// Chat-completions attempts get two, in this order: retryWithResponses runs
// before the param retry, because a Responses-required 400 names
// reasoning_effort, which the param parser would otherwise learn as a strip.
// The param retry rebuilds the OpenAI-shaped st.bodyBytes and re-POSTs it to
// the same targetURL, which for a native /v1/messages or generateContent route
// would be malformed.
//
// A /v1/responses attempt gets the param retry too: OpenAI names the parameter
// there by the same quoted name it uses on chat-completions ("Unsupported
// parameter: 'temperature' is not supported with this model"), and the retry
// rebuilds the body in the Responses dialect. A pro-tier model is served by
// that route alone, so without this a client sending temperature cannot reach
// it. responsesRejectedParams keeps the one ambiguous name from being
// mislearned onto the compat path.
//
// Anthropic egress attempts get the one 400 that route can fix by asking
// differently: a model refusing the extended-thinking shape it was asked in.
// Every other Messages 400 is left alone.
func (h *Handler) retryLearnable400(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType, targetURL string,
	resp *http.Response,
	attempt int,
	dialMs *float64,
	failoverCancel context.CancelFunc,
	streamCancelOrigin string,
) (paramRetryResult, bool) {
	switch {
	case st.anthropicEgressAttempt:
		return h.retryLearnableMessages400(r, st, candidate, providerType, resp, attempt, dialMs, failoverCancel, streamCancelOrigin)
	case st.sentChatCompletionsBody():
		res, handled := h.retryWithResponses(r, st, candidate, providerType, resp, attempt, dialMs, failoverCancel, streamCancelOrigin)
		switch {
		case !handled:
			res = h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, dialMs, failoverCancel, streamCancelOrigin)
		case res.retried && res.resp != nil && res.resp.StatusCode == http.StatusBadRequest:
			// The rebuilt Responses request was refused in turn. It is a
			// Responses attempt now (retryWithResponses marked it), so the
			// param self-heal runs on the reroute's own 400, letting a
			// first request to a pro-tier model carrying temperature heal
			// inside one attempt. The reroute's context owns the body being
			// read; the learner releases it once the body is buffered.
			res = h.retryWithStrippedParams(r, st, candidate, providerType, responsesTargetURL(candidate, providerType), res.resp, attempt, dialMs, res.retryCancel, res.streamCancelOrigin)
			// The reroute was a retry whatever the chained self-heal did
			// next, and the caller folds the retries' dial time on this flag.
			res.retried = true
		}
		return res, true
	case st.responsesAttempt:
		return h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, dialMs, failoverCancel, streamCancelOrigin), true
	}
	return paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}, false
}

// learnRejectedParams caches the params and renames that a 400 body names, so later
// requests to the same provider+model are built without them. It is the
// learning half of retryWithStrippedParams, for callers that cannot use it:
// a hedged probe, which must not spend a second round-trip inside one race
// slot, and the Messages path, which rebuilds in its own dialect.
func (h *Handler) learnRejectedParams(st *requestState, candidate modelCandidate, body []byte) {
	h.mergeLearnedParams(candidate, learnableRejections(st, body), paramrewrite.ParseProviderParamRename(body))
}

// learnableRejections reads a 400 for the params it rejects, keeping the
// schema fallback only for a chat-completions request that sent json_schema:
// a native dialect's 400 keeps its response format elsewhere, and a JSON-mode
// request refused for its prompt is not healed by the fallback, so a retry
// built on it would repeat the same 400.
func learnableRejections(st *requestState, body []byte) map[string]bool {
	rejected := paramrewrite.ParseProviderParamError(body)
	if !st.sentChatCompletionsBody() {
		return paramrewrite.WithoutParams(rejected, paramrewrite.SchemaFallbackKey)
	}
	return paramrewrite.DropSchemaFallbackUnlessRequested(rejected, st.bodyBytes)
}

// responsesRejectedParams reads a Responses-dialect 400 for the param learner.
// "reasoning" is the one name both dialects carry that means different things:
// on chat-completions it is a caller's object, on the Responses body the
// translator regenerates it from reasoning_effort. The learned scope is
// provider+model and shared by both dialects, so a strip learned from that 400
// would delete the caller's object on the compat path.
//
// json_schema lives under text.format on the Responses body, so a 400 there
// is about that dialect's field; the fallback key is shared with the compat
// path and must not be learned from it either.
func responsesRejectedParams(body []byte) map[string]bool {
	return paramrewrite.WithoutParams(paramrewrite.ParseProviderParamError(body), "reasoning", paramrewrite.SchemaFallbackKey)
}

// mergeLearnedParams is the caching half of the param learner, shared by the
// dialect-specific readers.
func (h *Handler) mergeLearnedParams(candidate modelCandidate, rejected map[string]bool, renames map[string]string) {
	if rejected == nil && renames == nil {
		return
	}
	cacheKey := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	if rejected != nil {
		paramrewrite.MergeLearnedParamCache(&h.deprecationCache, cacheKey, rejected)
	}
	if renames != nil {
		paramrewrite.MergeLearnedParamCache(&h.paramRenameCache, cacheKey, renames)
	}
	debuglog.Info("proxy: learned rejected params from upstream 400", "provider", candidate.provider.Name, "model", candidate.model.ModelID, "rejected", fmt.Sprintf("%v", rejected), "renames", fmt.Sprintf("%v", renames))
}

// paramRetryRounds caps how many times one candidate is re-issued with newly
// learned params stripped.
//
// Upstreams name a single offending param per 400, so a request carrying two of
// them needs a second round to get through. Past that the provider is objecting
// to something this self-heal cannot fix, and the 400 belongs to the client.
const paramRetryRounds = 2

// retryMinRound is the least budget a self-heal round is issued with. A round
// cut shorter than this cannot finish a connect and a first byte, so it would
// only have the provider start work the gateway then abandons. 100ms matches
// failoverBackoff, the loop's own smallest interval.
const retryMinRound = 100 * time.Millisecond

// issueParamRetry rebuilds the upstream body with strip applied on top of the
// learned caches and re-POSTs it to targetURL, in the dialect the attempt
// spoke: chat-completions as sent, or the Responses translation of it.
//
// The returned cancel func belongs to the retry's context: non-nil exactly when
// a response is returned, whose body the caller must consume before calling it.
// On failure the context is cancelled here (there is no body to read) and the
// structured cause is returned for the failover loop to record.
func (h *Handler) issueParamRetry(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType, targetURL string,
	strip map[string]bool,
	attempt int,
	dialMs *float64,
) (*http.Response, context.CancelFunc, *reqError) {
	// Rebuild through the shared rewrite path, so stream_options injection,
	// provider injection, universal and learned param stripping and learned
	// renaming all apply on retry too. The renames just cached come from
	// paramRenameCache; the rejected params learned so far are passed as
	// extraStrip so the retry drops them before the cache writes are observed.
	rebuilt, err := h.rebuildForParamRetry(st, candidate, providerType, strip)
	if err != nil {
		return nil, nil, &reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)}
	}
	retryCtx, rc := retryContext(r, st)
	retryCtx, retryDial := withDialTiming(retryCtx)
	retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
	if retryErr != nil {
		rc()
		return nil, nil, &reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
	}
	util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
	retryReq.Header.Set("Content-Type", "application/json")
	var retryCheckRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		retryCheckRedirect = h.safeDialer.CheckRedirect
	}
	retryClient := &http.Client{Transport: h.upstreamTransport, CheckRedirect: retryCheckRedirect}
	// retryResp.Body is returned to the caller, which consumes and closes it
	retryResp, retryErr := retryClient.Do(retryReq)
	*dialMs += retryDial.take()
	if retryErr == nil && st.responsesAttempt {
		metrics.RecordResponsesReroute(candidate.provider.Name, candidate.model.ModelID, "param_retry")
	}
	if retryErr != nil {
		rc() // no body to consume on retry error
		debuglog.Warn("proxy: auto-retry request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", retryErr)
		if errors.Is(retryErr, context.Canceled) || errors.Is(retryErr, context.DeadlineExceeded) {
			// Branch like the main failover loop: Canceled = client
			// disconnect, DeadlineExceeded = retry timeout.
			origin := "retry_timeout"
			if errors.Is(retryErr, context.Canceled) {
				origin = "client_disconnect"
			}
			return nil, nil, &reqError{Kind: cancelOriginToKind(origin), Attempt: attempt, Provider: candidate.provider.Name}
		}
		return nil, nil, &reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
	}
	return retryResp, rc, nil
}

// rebuildForParamRetry builds the retry's body in the attempt's dialect, with
// the params just learned stripped ahead of the cache writes being observed.
// The Responses translation runs on the rewritten chat body and injects no
// stream_options, since the Responses API has its own streaming usage
// semantics.
func (h *Handler) rebuildForParamRetry(st *requestState, candidate modelCandidate, providerType string, strip map[string]bool) ([]byte, error) {
	if st.responsesAttempt {
		cleaned := paramrewrite.BuildNativeUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, &h.deprecationCache, &h.paramRenameCache, strip, learnedScopeFor(candidate))
		return openairesponses.TranslateChatToResponses(cleaned, candidate.model.ModelID)
	}
	return paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, strip, learnedScopeFor(candidate)), nil
}

// retryContext is the context one self-heal round runs under: the attempt's
// failover budget, cut at the request's overall deadline. Every round opens a
// fresh budget and an attempt can chain several, while the loop consults the
// overall deadline only between attempts, so without the cut one candidate
// could hold the request for several budgets past the point the loop would have
// given up. A state that never set the deadline keeps the plain budget.
func retryContext(r *http.Request, st *requestState) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(st.failoverTimeout)
	if !st.overallDeadline.IsZero() && st.overallDeadline.Before(deadline) {
		deadline = st.overallDeadline
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	return context.WithValue(ctx, ctxkeys.CancelOriginKey, "retry_timeout"), cancel
}

// readLearnable400 consumes a 400 body for the param self-heal: it reads what
// the learner can parse, drains the rest so the connection stays reusable,
// closes the original body and restores a readable one in its place. The
// response is handed on to whoever renders the client's error, so it must never
// leave here empty.
//
// The bound is the parse-sized responsesLearnBodyCap rather than the scan-sized
// failoverErrorClassifyCap: learning json.Unmarshals the whole document, so a
// body cut mid-JSON teaches nothing. Everything above the cap is discarded.
func readLearnable400(resp *http.Response) ([]byte, error) {
	limit := int64(responsesLearnBodyCap)
	if resp.StatusCode != http.StatusBadRequest {
		limit = failoverErrorClassifyCap
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, err
}

// retryWithStrippedParams handles a 400 from an upstream: it reads and restores
// the error body, cancels the original (now-finished) request context, and,
// when the body is a recognizable param rejection, learns the rejected params
// and renames into the deprecation/rename caches, rebuilds the request with
// them applied and re-issues it. A retry that is itself rejected with a 400 is
// read and learned from the same way, and re-issued once more when it names a
// param the retry did not already carry.
//
// failoverCancel is the original request's cancel func; it is invoked here once
// the 400 body has been consumed. dialMs is the per-request dial-timing pointer
// threaded into every retry context so SafeDialer records the retries' DNS time.
func (h *Handler) retryWithStrippedParams(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType, targetURL string,
	resp *http.Response,
	attempt int,
	dialMs *float64,
	failoverCancel context.CancelFunc,
	streamCancelOrigin string,
) paramRetryResult {
	// Restored before anything else, so downstream error handling can read the
	// body on every path that gives up without retrying.
	body, readErr := readLearnable400(resp)
	failoverCancel() // 400 body consumed, context no longer needed
	debuglog.Debug("proxy: received 400 from upstream, checking for param rejection", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "body_length", len(body))

	res := paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}
	if readErr != nil {
		return res
	}

	// Everything applied to the outgoing body so far, accumulated across rounds:
	// strip holds the rejected params (dropped), renamed the moves (value kept
	// under the new name). They are also the termination guard: a round is only
	// worth issuing when a 400 names something outside these sets.
	strip := map[string]bool{}
	renamed := map[string]string{}

	// A 400 can ask for a param to be dropped and/or moved to a new name (value
	// preserved). Both feed the same self-heal: learn, cache for future
	// preemptive application, and retry. learnFrom does the learning half for
	// one 400 body and reports whether that body named anything the request does
	// not already carry, that is, whether re-issuing could help.
	learnFrom := func(errBody []byte) bool {
		var rejected map[string]bool
		if st.responsesAttempt {
			rejected = responsesRejectedParams(errBody)
		} else {
			rejected = learnableRejections(st, errBody)
		}
		renames := paramrewrite.ParseProviderParamRename(errBody)
		if rejected == nil && renames == nil {
			return false
		}
		// Cache the learned rejections and renames for future preemptive
		// application. Each cache merges with its existing entries via
		// CompareAndSwap, so concurrent goroutines never mutate the same map.
		h.mergeLearnedParams(candidate, rejected, renames)
		progressed := false
		for p := range rejected {
			if !strip[p] {
				progressed = true
			}
			strip[p] = true
		}
		for oldName, newName := range renames {
			if _, seen := renamed[oldName]; !seen {
				progressed = true
			}
			renamed[oldName] = newName
		}
		return progressed
	}

	// Two guards, both required, so the loop always terminates: at most
	// paramRetryRounds rounds, and a round only when the current 400 names
	// something the request does not already carry (learnFrom's report), which
	// makes strip and renamed grow strictly on every iteration.
	//
	// learnFrom is the left operand so it is evaluated even on the iteration
	// that ends the loop, which is what makes the last 400 learned rather than
	// discarded: the round it would have paid for is refused, but the next
	// request still starts with what it named. A 400 that names nothing
	// recognisable falls straight out and is handed back untouched.
	for round := 0; learnFrom(body) && round < paramRetryRounds && st.retryBudgetLeft(); round++ {
		res.streamCancelOrigin = "retry_timeout"
		retryResp, rc, retryErr := h.issueParamRetry(r, st, candidate, providerType, targetURL, strip, attempt, dialMs)
		if retryErr != nil {
			res.lastReqErr = *retryErr
			res.cont = true
			return res
		}
		if retryResp.StatusCode != http.StatusBadRequest {
			// rc must not be called here: retryResp.Body is read by the caller,
			// which cancels once it has consumed the body.
			res.resp = retryResp
			res.retryCancel = rc
			res.retried = true
			debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rounds", round+1, "rejected_params", mapKeys(strip), "renamed_params", renameKeys(renamed))
			return res
		}
		// The retry was rejected too. Read its body so the params it names are
		// learned as well, then hand that response on with the body restored: it
		// is the client's 400 if no further round is issued.
		body, readErr = readLearnable400(retryResp)
		rc() // retry body consumed and buffered, context no longer needed
		res.resp = retryResp
		res.retryCancel = nil
		res.retried = true
		if readErr != nil {
			return res
		}
	}
	return res
}

// renameKeys returns the old param names from a rename map, for log fields.
func renameKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
