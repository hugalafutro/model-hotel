package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The OpenAI Responses re-route (plan: plans/openai-responses-endpoint.md).
//
// OpenAI's newest models reject tools+reasoning over /v1/chat/completions with
// a 400 that names /v1/responses as the forward path. The proxy self-heals the
// same way the param-strip retry does (learn from the 400, retry once), then
// caches the requirement per model so subsequent tools+reasoning requests for
// that model route to /v1/responses preemptively — hybrid strategy C of the
// plan: no repeated 400 round-trips. The pro tier is served by /v1/responses
// alone and refuses the chat endpoint with a 404; that refusal is learned the
// same way for every request to the model, and the tier's names route there
// from the first request on OpenAI's own host (see responsesRequirement).

// responsesCacheKey mirrors the paramrewrite cache keying.
func responsesCacheKey(providerType, modelID string) string {
	return providerType + ":" + modelID
}

// shouldUseResponsesAttempt reports whether this candidate must be served via
// /v1/responses: a direct-OpenAI chat attempt whose model is known to require
// it. A model learned to refuse tools+reasoning goes there only on a request
// carrying that combination (tools + reasoning not "none"); plain,
// reasoning-only and tools-off requests keep the cheaper chat-completions
// path. A model known to live behind /v1/responses alone goes there for every
// request.
func (h *Handler) shouldUseResponsesAttempt(st *requestState, candidate modelCandidate, providerType string) bool {
	if providerType != "openai" || st.endpointPath != "" || st.makeUpstreamBody != nil {
		return false
	}
	switch h.responsesRequirement(providerType, candidate.model.ModelID, candidate.provider.BaseURL) {
	case responsesAlways:
		return true
	case responsesForTools:
		return openairesponses.NeedsResponsesRouting(st.bodyBytes)
	}
	return false
}

// The two learned requirements (see responsesRequiredCache).
const (
	responsesForTools = "tools"
	responsesAlways   = "always"
)

// responsesRequirement is what the cache holds for the model, or the name rule
// for a model that has not been tried yet: the pro tier is Responses-only by
// construction, and routing it there from the first request saves the 404 that
// would otherwise teach it. The name rule applies on OpenAI's own host only:
// "openai" is also the type of every unrecognised OpenAI-compatible host, and
// a relay re-exposing a pro model over chat-completions has no /v1/responses
// to fall back from, so there the model is learned from its refusal or not at
// all.
func (h *Handler) responsesRequirement(providerType, modelID, baseURL string) string {
	if v, ok := h.responsesRequiredCache.Load(responsesCacheKey(providerType, modelID)); ok {
		if s, ok := v.(string); ok {
			return s
		}
		return responsesForTools
	}
	if isOpenAIHost(baseURL) && openairesponses.ResponsesOnlyModel(modelID) {
		return responsesAlways
	}
	return ""
}

// isOpenAIHost reports a base URL on api.openai.com: the one place the pro
// tier's names and the chat endpoint's 404 refusal mean what OpenAI means by
// them. An Azure deployment is its own provider type and never reaches this;
// its deployment names are operator-chosen, so neither rule would be safe
// there.
func isOpenAIHost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	return err == nil && strings.EqualFold(u.Hostname(), "api.openai.com")
}

// buildResponsesRequest builds the upstream request for a /v1/responses
// attempt. The chat body is pre-cleaned through the shared rewrite path first
// so learned param strips/renames (e.g. an unsupported temperature, max_tokens
// -> max_completion_tokens) still apply before translation — the Responses
// path has no param-strip self-heal of its own.
func (h *Handler) buildResponsesRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/responses")
	body, err := h.translateResponsesRequestBody(st, candidate, providerType)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	debuglog.Info("proxy: routing via responses api", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "stream", st.isStreaming)
	metrics.RecordResponsesReroute(candidate.provider.Name, candidate.model.ModelID, "preemptive")

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// translateResponsesRequestBody produces the /v1/responses body for one
// candidate: shared chat rewrite (model rename, learned strips/renames;
// isStreaming=false so no stream_options is injected — the Responses API has
// its own streaming usage semantics), then chat -> Responses translation.
func (h *Handler) translateResponsesRequestBody(st *requestState, candidate modelCandidate, providerType string) ([]byte, error) {
	cleaned := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, false, &h.deprecationCache, &h.paramRenameCache, nil, learnedScopeFor(candidate))
	return openairesponses.TranslateChatToResponses(cleaned, candidate.model.ModelID)
}

// retryWithResponses handles a chat-completions refusal that demands the
// Responses API, the tools+reasoning 400 or the pro tier's 404: learn the
// requirement into responsesRequiredCache, rebuild the request
// as a /v1/responses call and re-issue it once, marking the attempt so the
// response dispatch translates the answer back. Returns handled=false — with
// the 400 body restored on resp for the param-strip retry to inspect — when
// the error is not the Responses rejection (or the request would not re-route
// anyway). The result contract matches retryWithStrippedParams.
func (h *Handler) retryWithResponses(
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
	if providerType != "openai" || st.endpointPath != "" || st.makeUpstreamBody != nil {
		return res, false
	}

	// Same bounded read as the param self-heal: this learner also json.Unmarshals
	// the whole error document and also has to hand the response on with a
	// readable body. Reading it unbounded here would defeat readLearnable400's
	// cap entirely, since every openai-type 400 passes through this function
	// first.
	body, readErr := readLearnable400(resp)
	// The helper closed the upstream body and left a buffered reader in its
	// place, so this close is a no-op — it is here because bodyclose only
	// recognises a close applied to the response value in the function that
	// received it, not the one inside readLearnable400.
	_ = resp.Body.Close()
	if readErr != nil || !h.learnResponsesRequirement(st, candidate, providerType, body) {
		return res, false
	}
	if !st.retryBudgetLeft() {
		// Learned for the next request; this one carries the refusal on
		// as it came (the body is restored), the way an unlearnable one
		// does, rather than a reroute that would time out on issue.
		return res, true
	}
	failoverCancel() // 400 body fully consumed, original context no longer needed

	targetURL := responsesTargetURL(candidate, providerType)
	rebuilt, err := h.translateResponsesRequestBody(st, candidate, providerType)
	if err != nil {
		res.lastReqErr = reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)}
		res.cont = true
		return res, true
	}

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
	retryResp, retryErr := (&http.Client{Transport: h.upstreamTransport, CheckRedirect: checkRedirect}).Do(retryReq)
	*dialMs += retryDial.take()
	if retryErr != nil {
		rc()
		debuglog.Warn("proxy: responses api retry failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", retryErr)
		if errors.Is(retryErr, context.Canceled) || errors.Is(retryErr, context.DeadlineExceeded) {
			origin := "retry_timeout"
			if errors.Is(retryErr, context.Canceled) {
				origin = "client_disconnect"
			}
			res.lastReqErr = reqError{Kind: cancelOriginToKind(origin), Attempt: attempt, Provider: candidate.provider.Name}
		} else {
			res.lastReqErr = reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
		}
		res.cont = true
		return res, true
	}
	st.responsesAttempt = true
	res.resp = retryResp
	res.retryCancel = rc
	res.retried = true
	debuglog.Info("proxy: responses api retry succeeded", "model", candidate.model.ModelID, "status", retryResp.StatusCode)
	metrics.RecordResponsesReroute(candidate.provider.Name, candidate.model.ModelID, "learned")
	return res, true
}

// responsesTargetURL is the provider's /v1/responses route, shared by the
// reroute and the param retry that may follow it on the same attempt.
func responsesTargetURL(candidate modelCandidate, providerType string) string {
	return util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/responses")
}

// learnResponsesRequirement inspects a chat-completions 400 error body and,
// when it is the Responses rejection on a request that would re-route, records
// the requirement in responsesRequiredCache. Shared by the sequential retry
// (which then re-issues in place) and the hedged probe, which cannot retry
// in-race — there the learned flag makes every subsequent request, hedged or
// sequential, route preemptively instead of 400ing again.
func (h *Handler) learnResponsesRequirement(st *requestState, candidate modelCandidate, providerType string, errBody []byte) bool {
	if st.responsesAttempt || providerType != "openai" || st.endpointPath != "" || st.makeUpstreamBody != nil {
		return false
	}
	key := responsesCacheKey(providerType, candidate.model.ModelID)
	if openairesponses.IsResponsesOnlyRejection(errBody) {
		// The whole model lives behind /v1/responses: learn it for every
		// request, whatever this one carried.
		h.responsesRequiredCache.Store(key, responsesAlways)
		debuglog.Info("proxy: learned responses-only model", "model", candidate.model.ModelID, "provider", candidate.provider.Name)
		return true
	}
	if !openairesponses.RequiresResponsesAPI(errBody) || !openairesponses.NeedsResponsesRouting(st.bodyBytes) {
		return false
	}
	h.responsesRequiredCache.Store(key, responsesForTools)
	debuglog.Info("proxy: learned responses api requirement", "model", candidate.model.ModelID, "provider", candidate.provider.Name)
	return true
}

// translateResponsesResponseBody swaps a non-streaming /v1/responses 200 body
// for its chat.completion translation so handleNonStreamingResponse can meter
// and forward it unchanged.
func translateResponsesResponseBody(resp *http.Response, model string) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	translated, err := openairesponses.TranslateResponsesToChat(body, model)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(translated))
	return nil
}
