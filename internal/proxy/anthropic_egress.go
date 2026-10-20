package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The Anthropic egress adapter. A chat request is translated to Anthropic's
// native /v1/messages shape on the way out (internal/anthropicegress) and the
// answer translated back on the upstream side of the pipeline — the same trick
// as the gemini egress adapter and the /v1/responses re-route, so the TTFT
// probe, stall watchdog, transforms and metering all run unchanged.
//
// Every chat request to a Messages speaker takes it, the Test button and the
// retirement probe included. Anthropic's own API also fronts an OpenAI-compat
// /v1/chat/completions, which the gateway does not use: that route carries no
// cache_control, so a multi-turn agent re-sending its whole context there pays
// full input price on every turn, where the Messages route with the breakpoints
// TranslateRequest places (caching.go) bills the repeated prefix at a tenth.

// isAnthropicFamily reports whether a provider type speaks Anthropic's native
// Messages API. Both members serve /v1/messages with x-api-key. "anthropic" is
// Anthropic's own API; "anthropic-messages" is an operator-entered endpoint that
// speaks Messages and nothing else. Chat traffic to either is translated.
func isAnthropicFamily(providerType string) bool {
	return providerType == "anthropic" || providerType == "anthropic-messages"
}

// isOpenCodeClaude is util.IsOpenCodeClaude: a Claude model behind one of the
// OpenCode types, which OpenCode serves over Anthropic Messages alone.
func isOpenCodeClaude(providerType, modelID string) bool {
	return util.IsOpenCodeClaude(providerType, modelID)
}

// speaksMessages reports a candidate reachable at the provider's /messages
// route: an Anthropic-family provider, or a Claude model on OpenCode.
func speaksMessages(providerType, modelID string) bool {
	return isAnthropicFamily(providerType) || isOpenCodeClaude(providerType, modelID)
}

// isAnthropicEgressAttempt reports whether this candidate is served through the
// Anthropic egress adapter: a plain chat-completions request (no explicit
// endpoint override, no multipart body) bound for a Messages-speaking candidate
// (an Anthropic-family provider, or a Claude model on OpenCode).
//
// An Anthropic-in request is excluded: it already has a better path
// (buildNativeAnthropicRequest forwards its original Messages body verbatim),
// and translating a body that is already Anthropic-shaped would corrupt it.
func isAnthropicEgressAttempt(st *requestState, providerType, modelID string) bool {
	if !speaksMessages(providerType, modelID) || st.anthropicIn {
		return false
	}
	// A non-chat surface (embeddings, audio, images) and a multipart body both
	// carry shapes the Messages API has no equivalent for, so neither can be
	// translated. They keep the direct route, which answers with the upstream's
	// own 404 for a surface it does not serve.
	return st.endpointPath == "" && st.makeUpstreamBody == nil
}

// buildAnthropicEgressRequest builds the upstream request for an Anthropic
// egress attempt. The chat body goes through the shared rewrite path first
// (model rename, learned strips; isStreaming=false so no stream_options is
// injected — Anthropic has no such knob), then chat -> Messages translation.
// One route serves both modes: Anthropic streams from /v1/messages too, with
// the body's "stream" field making the difference.
func (h *Handler) buildAnthropicEgressRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	body, model, stream, err := h.anthropicEgressBody(st, candidate, providerType, h.thinkingDialectFor(candidate))
	if err != nil {
		return nil, providerType, "", err
	}
	// Kept for the self-heal to compare a rebuild against (see
	// learnAndRebuildMessages400); overwritten per attempt, like the dialect flags.
	st.lastMessagesBody = body

	proxyReq, targetURL, err := newMessagesRequest(ctx, st, candidate, providerType, body)
	debuglog.Info("proxy: routing via anthropic egress adapter", "target_url", targetURL, "model", model, "provider", candidate.provider.Name, "stream", stream)
	if err != nil {
		return nil, providerType, targetURL, err
	}
	return proxyReq, providerType, targetURL, nil
}

// newMessagesRequest builds the POST every Messages route makes: the
// provider's /messages target URL, the body, and the headers the route reads
// (SetMessagesAuthHeaders: x-api-key + anthropic-version for every Messages
// speaker, and for OpenCode also the Go session header its plan wants on every
// call). The target URL comes back even on failure, since the callers log it
// either way.
func newMessagesRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string, body []byte) (*http.Request, string, error) {
	targetURL := messagesTargetURL(candidate, providerType)
	req, err := newJSONUpstreamRequest(ctx, targetURL, body)
	if err != nil {
		return nil, targetURL, err
	}
	util.SetMessagesAuthHeaders(req, providerType, candidate.apiKey)
	util.SetOpenCodeGoSession(req, providerType, st.opencodeSession)
	return req, targetURL, nil
}

// anthropicEgressBody builds the Messages body for one egress attempt: the
// shared chat rewrite (model rename, learned strips) followed by translation in
// the given thinking dialect. The initial attempt and the dialect retry both go
// through here so a change to either half cannot apply to only one of them.
func (h *Handler) anthropicEgressBody(st *requestState, candidate modelCandidate, providerType string, dialect anthropicegress.ThinkingDialect) (body []byte, model string, stream bool, err error) {
	// Every Messages body is built with the anthropic-messages parameter rules,
	// whichever type the candidate is: top_p is dropped as Anthropic now rejects
	// it, and reasoning_effort is kept for the translator to turn into thinking.
	// The OpenCode types' own rule strips reasoning_effort because their chat
	// route rejects it, which is a route this body never reaches.
	cleaned := paramrewrite.BuildNativeUpstreamBody(st.bodyBytes, "anthropic-messages", candidate.model.ModelID, st.reqModel, &h.deprecationCache, &h.paramRenameCache, nil, learnedScopeFor(candidate))
	opts := anthropicegress.Options{Dialect: dialect, Caching: h.cachingFor(candidate)}
	if candidate.model.MaxOutputTokens != nil {
		opts.DefaultMaxTokens = *candidate.model.MaxOutputTokens
	}
	return anthropicegress.TranslateRequestWithOptions(cleaned, opts)
}

// buildErrorKind classifies a buildCandidateRequest failure: a body the
// Messages translation refuses (a file_id, an audio part, no usable turn) is
// the caller's doing and books as a validation error, so another candidate
// with a route for it still gets its turn and the terminal answer, if none has
// one, is a 400; anything else is this gateway's fault. The sequential loop
// and the hedged probe share it so they can never disagree.
func buildErrorKind(err error) ErrorKind {
	if errors.Is(err, anthropicegress.ErrInvalidRequest) {
		return KindValidation
	}
	return KindInternal
}

// cachingFor reports whether this candidate's Messages requests carry
// prompt-cache breakpoints: yes unless a 400 from this provider+model taught
// that its endpoint refuses the cache_control field (see
// anthropicegress.CacheControlRefused and learnCacheRefusal).
func (h *Handler) cachingFor(candidate modelCandidate) bool {
	key := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	_, refused := h.cacheRefusedCache.Load(key)
	return !refused
}

// learnCacheRefusal records that this provider+model's Messages endpoint rejects
// cache_control, so later requests to it are built without breakpoints from the
// start. Logged at warn: every request to that model now pays full price for
// its prompt, which the operator may want to know about the endpoint.
func (h *Handler) learnCacheRefusal(candidate modelCandidate) {
	key := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	if _, already := h.cacheRefusedCache.LoadOrStore(key, true); !already {
		debuglog.Warn("proxy: anthropic messages endpoint refused cache_control; prompt caching off for this model", "provider", candidate.provider.Name, "model", candidate.model.ModelID)
	}
}

// thinkingDialectFor returns the extended-thinking shape to ask this candidate's
// model for: whatever a previous 400 taught, else the adaptive default. The key
// is the provider scope plus the upstream model id, the same one the learned
// param caches use, because the fact is per model AND per provider — the same
// model id behind two Messages endpoints need not be the same build.
func (h *Handler) thinkingDialectFor(candidate modelCandidate) anthropicegress.ThinkingDialect {
	key := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	if v, ok := h.thinkingDialectCache.Load(key); ok {
		if dialect, isDialect := v.(anthropicegress.ThinkingDialect); isDialect {
			return dialect
		}
	}
	return anthropicegress.ThinkingAdaptive
}

// learnThinkingDialect records the shape an upstream 400 asked for, so later
// requests to the same provider+model are built with it from the start.
func (h *Handler) learnThinkingDialect(candidate modelCandidate, dialect anthropicegress.ThinkingDialect) {
	key := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	h.thinkingDialectCache.Store(key, dialect)
	debuglog.Info("proxy: learned anthropic thinking dialect from upstream 400", "provider", candidate.provider.Name, "model", candidate.model.ModelID, "dialect", dialect.String())
}

// messagesTargetURL is the provider's Anthropic Messages endpoint.
func messagesTargetURL(candidate modelCandidate, providerType string) string {
	return util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/messages")
}
