package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The OpenAI Responses re-route.
//
// OpenAI's newest models reject tools+reasoning over /v1/chat/completions with
// a 400 that names /v1/responses as the forward path. The proxy self-heals the
// same way the param-strip retry does (learn from the 400, retry once), then
// caches the requirement per model so subsequent tools+reasoning requests for
// that model route to /v1/responses preemptively, without repeating the 400
// round-trip. The pro tier is served by /v1/responses alone and refuses the
// chat endpoint with a 404; that refusal is learned the same way for every
// request to the model, and the tier's names route there from the first request
// on OpenAI's own host. OpenCode Zen and Go serve their GPT models the same
// way (a 400 naming the protocol) and are rerouted through the same learner.

// responsesCacheKey mirrors the paramrewrite cache keying.
func responsesCacheKey(providerType, modelID string) string {
	return providerType + ":" + modelID
}

// shouldUseResponsesAttempt reports whether this candidate must be served via
// /v1/responses: a chat attempt on OpenAI or one of the OpenCode types whose
// model is known to require it. A model learned to refuse tools+reasoning goes there only on a request
// carrying that combination (tools + reasoning not "none"); plain,
// reasoning-only and tools-off requests keep the cheaper chat-completions
// path. A model known to live behind /v1/responses alone goes there for every
// request.
func (h *Handler) shouldUseResponsesAttempt(st *requestState, candidate modelCandidate, providerType string) bool {
	if !st.plainOpenAIChat(providerType) {
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

// The two requirements responsesRequiredCache can hold for a model.
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
// attempt. The chat body is pre-cleaned through the shared rewrite path first,
// so learned param strips and renames (e.g. an unsupported temperature,
// max_tokens -> max_completion_tokens) apply before translation.
func (h *Handler) buildResponsesRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	targetURL := responsesTargetURL(candidate, providerType)
	body, err := h.translateResponsesRequestBody(st, candidate, providerType)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	debuglog.Info("proxy: routing via responses api", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "stream", st.isStreaming)
	metrics.RecordResponsesReroute(candidate.provider.Name, candidate.model.ModelID, "preemptive")

	proxyReq, err := newJSONUpstreamRequest(ctx, targetURL, body)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	util.SetOpenCodeGoSession(proxyReq, providerType, st.opencodeSession)
	return proxyReq, providerType, targetURL, nil
}

// translateResponsesRequestBody produces the /v1/responses body for one
// candidate: shared chat rewrite (model rename, learned strips and renames,
// isStreaming=false so no stream_options is injected, since the Responses API
// has its own streaming usage semantics), then chat to Responses translation.
// The OpenCode types strip reasoning_effort on the chat route, where their
// other models reject it; the GPT models behind /v1/responses take it, so the
// client's effort is put back before translation, unless a Responses 400 from
// this very provider and model has since taught the param learner to strip
// it, in which case that lesson stands.
func (h *Handler) translateResponsesRequestBody(st *requestState, candidate modelCandidate, providerType string) ([]byte, error) {
	scope := learnedScopeFor(candidate)
	cleaned := paramrewrite.BuildNativeUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, &h.deprecationCache, &h.paramRenameCache, nil, scope)
	if isOpenCodeType(providerType) && !h.learnedStrip(scope, candidate.model.ModelID, "reasoning_effort") {
		cleaned = restoreReasoningEffort(cleaned, st.bodyBytes)
	}
	return openairesponses.TranslateChatToResponses(cleaned, candidate.model.ModelID)
}

// learnedStrip reports whether a 400 has taught the param learner to strip
// param for this provider and model.
func (h *Handler) learnedStrip(scope, modelID, param string) bool {
	return paramrewrite.CachedRejectedParams(&h.deprecationCache, paramrewrite.LearnedCacheKey(scope, modelID))[param]
}

// isOpenCodeType reports the two OpenCode provider types, whose GPT models are
// served over /v1/responses alone.
func isOpenCodeType(providerType string) bool {
	return providerType == "opencode-go" || providerType == "opencode-zen"
}

// isOpenCodeResponsesRefusal reports an OpenCode protocol refusal that means
// "use /v1/responses": the OpenCode types only, since an openai-typed relay
// answering the same words has no Responses route the reroute could learn;
// and their GPT models only, since the same body would also refuse a model
// OpenCode serves over a third protocol, and learning that one as
// Responses-only would pin it to a route that refuses it too.
func isOpenCodeResponsesRefusal(providerType, modelID string, errBody []byte) bool {
	return isOpenCodeType(providerType) && strings.HasPrefix(modelID, "gpt-") && openairesponses.IsOpenCodeProtocolRefusal(errBody)
}

// restoreReasoningEffort copies the client's reasoning_effort back onto a
// cleaned chat body that a provider-type strip removed it from. Either body
// failing to parse leaves the cleaned one as it is.
func restoreReasoningEffort(cleaned, original []byte) []byte {
	var want struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if json.Unmarshal(original, &want) != nil || want.ReasoningEffort == "" {
		return cleaned
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(cleaned, &m) != nil {
		return cleaned
	}
	if _, ok := m["reasoning_effort"]; ok {
		return cleaned
	}
	effort, err := json.Marshal(want.ReasoningEffort)
	if err != nil {
		return cleaned
	}
	m["reasoning_effort"] = effort
	out, err := json.Marshal(m)
	if err != nil {
		return cleaned
	}
	return out
}

// retryWithResponses handles a chat-completions refusal that demands the
// Responses API, the tools+reasoning 400, the pro tier's 404 or OpenCode's
// protocol 400 on a GPT model: learn the
// requirement into responsesRequiredCache, rebuild the request as a
// /v1/responses call and re-issue it once, marking the attempt so the response
// dispatch translates the answer back. When the error is not the Responses
// rejection (or the request would not re-route anyway) it returns
// handled=false, with the 400 body restored on resp for the param-strip retry
// to inspect. The result contract matches retryWithStrippedParams.
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
	if !st.plainOpenAIChat(providerType) {
		return res, false
	}

	// Same bounded read as the param self-heal: this learner also json.Unmarshals
	// the whole error document and also has to hand the response on with a
	// readable body. Reading it unbounded here would defeat readLearnable400's
	// cap entirely, since every openai-type 400 passes through this function
	// first.
	body, readErr := readLearnable400(resp)
	// The helper closed the upstream body and left a buffered reader in its
	// place, so this close is a no-op. It is here because bodyclose only
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

	res.streamCancelOrigin = "retry_timeout"
	//nolint:bodyclose // retry resp.Body is consumed by the caller's dispatch
	retryResp, rc, reqErr, ok := h.issueRetry(r, st, candidate, providerType, targetURL, rebuilt, attempt, dialMs, "proxy: responses api retry failed")
	if !ok {
		res.lastReqErr = reqErr
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
// in-race: there the learned flag makes every subsequent request, hedged or
// sequential, route preemptively instead of 400ing again.
func (h *Handler) learnResponsesRequirement(st *requestState, candidate modelCandidate, providerType string, errBody []byte) bool {
	if st.responsesAttempt || !st.plainOpenAIChat(providerType) {
		return false
	}
	key := responsesCacheKey(providerType, candidate.model.ModelID)
	if openairesponses.IsResponsesOnlyRejection(errBody) || isOpenCodeResponsesRefusal(providerType, candidate.model.ModelID, errBody) {
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
// and forward it unchanged. It goes through the shared egress read so the
// Responses route is bounded by nonStreamingBodyCap like every other
// non-streaming translation; the Responses translator mints its own id, so the
// id and created arguments go unused.
func translateResponsesResponseBody(resp *http.Response, model string) error {
	return translateEgressResponseBody(resp, model, func(body []byte, _, model string, _ int64) ([]byte, error) {
		return openairesponses.TranslateResponsesToChat(body, model)
	})
}

// plainOpenAIChat reports an attempt that is a chat-completions call in the
// OpenAI dialect against a provider with a /v1/responses route to fall back
// to: OpenAI itself, and OpenCode Zen and Go, which serve their GPT models
// behind that route alone. Not a pass-through endpoint and not a translated
// dialect. It is the precondition for every part of the Responses reroute,
// since only such an attempt has a chat body to translate and an answer to
// translate back.
func (st *requestState) plainOpenAIChat(providerType string) bool {
	return (providerType == "openai" || isOpenCodeType(providerType)) && st.endpointPath == "" && st.makeUpstreamBody == nil
}
