package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// responsesLearnBodyCap bounds a 400 that has to be parsed rather than scanned.
// A learnable 404 is read at the classifier's cap instead: the pro tier's
// refusal is a couple of hundred bytes, and a 404 of any other make is a
// body the pipeline discards.
//
// openairesponses.RequiresResponsesAPI json.Unmarshals the whole error document,
// so the classifier's window is the wrong size for it: a body cut off mid-JSON
// does not parse, and the /v1/responses requirement would silently stop being
// learned. A megabyte is far past any real 400 and still bounded.
const responsesLearnBodyCap = 1 << 20

// paramRetryResult is the outcome of the 400 param-stripping auto-retry
// (retryWithStrippedParams). It tells the failover loop how to proceed:
//   - resp: the response to continue handling with — the last retry's response
//     once any retry was issued, otherwise the original 400. Every 400 that
//     leaves here carries a restored body, so normal non-200 handling can read
//     it whether it is the original or the one a retry earned.
//   - retryCancel: the retry context's cancel func, non-nil only when a retry
//     response is live and its body has NOT yet been consumed. The caller must
//     call it after consuming the body.
//   - streamCancelOrigin: "retry_timeout" once a retry was issued, otherwise
//     the caller's original value, unchanged.
//   - retried: true once a retry request was issued and answered, whatever it
//     answered with — the caller must fold the retries' dial time into the
//     running totals.
//   - lastReqErr: set only when cont is true; the structured cause the caller
//     records via st.setReqErr before failing over.
//   - cont: true => the caller should `continue` to the next candidate (a retry
//     request could not be created or failed).
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

// candidateModelID reads a candidate's upstream model id for diagnostics.
// recordBreakerOutcome is reached on paths that only carry the provider, so the
// model must never be assumed present.
//
// The empty string it falls back to is not inert: the breaker keys a circuit by
// this id, so a modelless candidate charges the "" circuit, and that circuit
// counts toward the span that indicts the provider. Every candidate the failover
// loop routes carries a model, so reaching this branch means a candidate was
// assembled without one and the breaker is now attributing that provider's
// health to a key nothing routes by. It is logged at error, with routing
// metadata only, because nothing downstream can tell the operator which model
// went missing.
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
// sent, and reports handled=false when none does. Each family can only read the
// dialect it speaks: a 400 names the fields of the request that earned it, so a
// reading taken from the wrong dialect is not merely useless but harmful — a
// param mislearned from a Messages 400 would poison the compat path for that
// model on every later request.
//
// Chat-completions attempts get two, in this order: retryWithResponses runs
// BEFORE the param retry because a Responses-required 400 names
// reasoning_effort, which the param parser would otherwise learn as a strip
// (useless on reason-by-default models). The param retry itself works
// universally — any LLM API naming "temperature" or "top_p" in a 400 can only
// mean the sampling parameter — but it rebuilds the OpenAI-shaped st.bodyBytes
// and re-POSTs it to the same targetURL, which for a native /v1/messages or
// generateContent route would be a malformed request.
//
// A /v1/responses attempt gets the param retry too: OpenAI names the
// parameter there by the same quoted name it uses on chat-completions
// ("Unsupported parameter: 'temperature' is not supported with this model"),
// and the retry rebuilds the body in the Responses dialect. A pro-tier model
// is served by that route alone, so without this a client sending
// temperature could never reach it at all. What keeps a Responses-only
// field from being mislearned onto the compat path is that the Responses
// body is a closed struct (openairesponses.Request) sharing only the
// sampling names with chat-completions, plus one exception handled by name:
// see responsesRejectedParams.
//
// Anthropic egress attempts get the one 400 that route can fix by asking
// differently: a model refusing the extended-thinking shape it was asked in
// (anthropic_thinking_retry.go). Every other Messages 400 is left alone.
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
		if !handled {
			res = h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, dialMs, failoverCancel, streamCancelOrigin)
		}
		return res, true
	case st.responsesAttempt:
		return h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, dialMs, failoverCancel, streamCancelOrigin), true
	}
	return paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}, false
}

// learnRejectedParams caches the params and renames a 400 body names, so later
// requests to the same provider+model are built without them. It is the
// learning half of retryWithStrippedParams, split out for callers that cannot
// retry in place (a hedged probe, which must not spend a second round-trip
// inside one race slot).
func (h *Handler) learnRejectedParams(candidate modelCandidate, body []byte) {
	h.mergeLearnedParams(candidate, paramrewrite.ParseProviderParamError(body), paramrewrite.ParseProviderParamRename(body))
}

// responsesRejectedParams reads a Responses-dialect 400 for the param
// learner. "reasoning" is the one name both dialects carry that means
// different things: on chat-completions it is a caller's object, on the
// Responses body the translator regenerates it from reasoning_effort on every
// request, so a strip learned from that 400 would delete the caller's object
// on the compat path (the learned scope is provider+model, shared by both
// dialects) and never fix the Responses request that taught it.
func responsesRejectedParams(body []byte) map[string]bool {
	rejected := paramrewrite.ParseProviderParamError(body)
	delete(rejected, "reasoning")
	if len(rejected) == 0 {
		return nil
	}
	return rejected
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
	// Rebuild the request body using the shared rewrite path. This ensures
	// stream_options injection, provider injection, universal/learned param
	// stripping, and learned renaming are all applied on retry, preventing drift
	// from the initial attempt path. The renames just cached are picked up from
	// paramRenameCache; the rejected params learned so far are also passed as
	// extraStrip so the retry drops them even before the cache writes are observed.
	rebuilt, err := h.rebuildForParamRetry(st, candidate, providerType, strip)
	if err != nil {
		return nil, nil, &reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)}
	}
	retryCtx, rc := context.WithTimeout(r.Context(), st.failoverTimeout)
	retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
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
	//nolint:bodyclose // retryResp.Body is returned to the caller, which consumes and closes it
	retryResp, retryErr := retryClient.Do(retryReq)
	*dialMs += retryDial.take()
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

// rebuildForParamRetry is the retry's body in the attempt's dialect. The
// Responses translation runs on the rewritten chat body the same way
// translateResponsesRequestBody does for a first attempt (no stream_options,
// the Responses API has its own streaming usage semantics), with the params
// just learned stripped ahead of the cache writes being observed.
func (h *Handler) rebuildForParamRetry(st *requestState, candidate modelCandidate, providerType string, strip map[string]bool) ([]byte, error) {
	if st.responsesAttempt {
		cleaned := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, false, &h.deprecationCache, &h.paramRenameCache, strip, learnedScopeFor(candidate))
		body, err := openairesponses.TranslateChatToResponses(cleaned, candidate.model.ModelID)
		if err != nil {
			return nil, err
		}
		metrics.RecordResponsesReroute(candidate.provider.Name, candidate.model.ModelID, "param_retry")
		return body, nil
	}
	return paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, strip, learnedScopeFor(candidate)), nil
}

// readLearnable400 consumes a 400 body for the param self-heal: it reads what
// the learner can parse, drains the rest so the connection stays reusable,
// closes the original body and restores a readable one in its place — the
// response is handed on to whoever renders the client's error, so it must never
// leave here empty.
//
// The bound is the parse-sized responsesLearnBodyCap rather than the
// scan-sized failoverErrorClassifyCap: learning json.Unmarshals the whole
// document, so a body cut mid-JSON parses to nothing and teaches nothing. The
// cap sits far past any real provider error, and everything above it is
// discarded rather than held.
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
// the error body, cancels the original (now-finished) request context, and — if
// the body is a recognizable param-rejection — learns the rejected params and
// renames into the deprecation/rename caches, rebuilds the request with them
// applied, and re-issues it. A retry that is itself rejected with a 400 is read
// and learned from in exactly the same way, and re-issued once more when it
// names a param the retry did not already carry. See paramRetryResult for how
// the loop interprets the return value.
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
	// under the new name). They are also the termination guard — a round is only
	// worth issuing when a 400 names something outside these sets.
	strip := map[string]bool{}
	renamed := map[string]string{}

	// A 400 can ask us to drop a param (rejected → strip) and/or move a param to
	// a new name (rename → preserve value). Both feed the same self-heal: learn,
	// cache for future preemptive application, and retry. learnFrom does the
	// learning half for one 400 body and reports whether that body named anything
	// the request does not already carry — i.e. whether re-issuing could help.
	learnFrom := func(errBody []byte) bool {
		var rejected map[string]bool
		if st.responsesAttempt {
			rejected = responsesRejectedParams(errBody)
		} else {
			rejected = paramrewrite.ParseProviderParamError(errBody)
		}
		renames := paramrewrite.ParseProviderParamRename(errBody)
		if rejected == nil && renames == nil {
			return false
		}
		// Cache the learned rejections and renames for future preemptive
		// application. Each cache is merged with any existing entries via
		// CompareAndSwap to avoid data races from concurrent goroutines mutating
		// the same map.
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
	// makes strip∪renamed grow strictly on every iteration.
	//
	// learnFrom runs before the round check on purpose — it is the left operand,
	// so it is evaluated even on the iteration that ends the loop. That is what
	// makes the last 400 learned rather than discarded: the round it would have
	// paid for is refused, but the next request still starts with what it named.
	//
	// The first 400 that names nothing recognisable falls straight out here and
	// is handed back untouched.
	for round := 0; learnFrom(body) && round < paramRetryRounds; round++ {
		res.streamCancelOrigin = "retry_timeout"
		retryResp, rc, retryErr := h.issueParamRetry(r, st, candidate, providerType, targetURL, strip, attempt, dialMs)
		if retryErr != nil {
			res.lastReqErr = *retryErr
			res.cont = true
			return res
		}
		if retryResp.StatusCode != http.StatusBadRequest {
			// rc must NOT be called here — retryResp.Body is read by the caller.
			// It is returned for deferred cleanup after body consumption.
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
