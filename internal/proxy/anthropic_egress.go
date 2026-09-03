package proxy

import (
	"bytes"
	"context"
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
// Two provider types need it, for opposite reasons. Anthropic's own OpenAI-compat
// /v1/chat/completions cannot express a document: an OpenAI {"type":"file"}
// content part is rejected with `messages.0.user.content.str: Input should be
// a valid string`, so those requests alone are translated. An
// "anthropic-messages" provider has no compat endpoint at all, so all of its
// chat traffic is.

// isAnthropicFamily reports whether a provider type speaks Anthropic's native
// Messages API. Both members serve /v1/messages with x-api-key; they differ in
// what else they serve. "anthropic" is Anthropic's own API, which also fronts an
// OpenAI-compatible /v1/chat/completions, so the compat endpoint remains its
// default route. "anthropic-messages" is an operator-entered endpoint that
// speaks Messages and nothing else, so every chat request there is translated.
func isAnthropicFamily(providerType string) bool {
	return providerType == "anthropic" || providerType == "anthropic-messages"
}

// isAnthropicEgressAttempt reports whether this candidate is served through the
// Anthropic egress adapter: a plain chat-completions request (no explicit
// endpoint override, no multipart body) bound for an Anthropic-family provider
// that has to be spoken to in Messages.
//
// The two types reach that conclusion differently. For "anthropic-messages"
// every chat request qualifies: the compat endpoint the untranslated body would
// go to does not exist there. For "anthropic" only a request carrying a content
// part the compat endpoint cannot express does, because that endpoint does exist
// and forwarding an untranslated body to it is both cheaper and more faithful
// than a round trip through the translator.
//
// An Anthropic-in request is excluded for both: it already has a better path
// (buildNativeAnthropicRequest forwards its original Messages body verbatim),
// and translating a body that is already Anthropic-shaped would corrupt it.
func isAnthropicEgressAttempt(st *requestState, providerType string) bool {
	if !isAnthropicFamily(providerType) || st.anthropicIn {
		return false
	}
	// A non-chat surface (embeddings, audio, images) and a multipart body both
	// carry shapes the Messages API has no equivalent for, so neither can be
	// translated. They keep the direct route, which answers with the upstream's
	// own 404 for a surface it does not serve.
	if st.endpointPath != "" || st.makeUpstreamBody != nil {
		return false
	}
	if providerType == "anthropic-messages" {
		return true
	}
	return anthropicegress.NeedsNativeRouting(st.bodyBytes)
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

	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, "/messages")
	debuglog.Info("proxy: routing via anthropic egress adapter", "target_url", targetURL, "model", model, "provider", candidate.provider.Name, "stream", stream)

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	// SetProviderAuthHeaders already sends x-api-key + anthropic-version for
	// this provider type, which is exactly what the native route requires.
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// anthropicEgressBody builds the Messages body for one egress attempt: the
// shared chat rewrite (model rename, learned strips) followed by translation in
// the given thinking dialect. The initial attempt and the dialect retry both go
// through here so a change to either half cannot apply to only one of them.
func (h *Handler) anthropicEgressBody(st *requestState, candidate modelCandidate, providerType string, dialect anthropicegress.ThinkingDialect) (body []byte, model string, stream bool, err error) {
	cleaned := paramrewrite.BuildNativeUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, &h.deprecationCache, &h.paramRenameCache, learnedScopeFor(candidate))
	return anthropicegress.TranslateRequestWithDialect(cleaned, dialect)
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
