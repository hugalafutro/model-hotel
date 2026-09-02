package proxy

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The gemini egress adapter. Vertex AI express-mode API keys only work on
// Google's native publisher routes, so a chat-completions request bound for
// such a provider is translated to generateContent shape on the way out
// (internal/gemini) and the response translated back on the upstream side of
// the pipeline, leaving the TTFT probe, stall watchdog, transforms and metering
// to run unchanged.

// isGeminiEgressAttempt reports whether this candidate is served through the
// gemini egress adapter: a plain chat-completions request (no explicit endpoint
// override, no multipart body) bound for a provider that only speaks Gemini's
// native dialect for this model.
//
// Three providers qualify. vertex-express speaks it for every model. OpenCode
// Zen speaks it for its Gemini models only: Zen routes each model family to a
// different upstream shape, and its Gemini models are a thin passthrough to
// Google, so an OpenAI-shaped body sent to Zen's /chat/completions comes back
// with Google's own `Invalid JSON request body: Missing key at ["contents"]`.
// Every other Zen family stays on chat-completions. Google AI Studio speaks it
// for image output only (see isGoogleImageEgress).
func isGeminiEgressAttempt(st *requestState, providerType, modelID, outputModalities string) bool {
	if st.endpointPath != "" || st.makeUpstreamBody != nil {
		return false
	}
	return providerType == "vertex-express" || isZenGeminiModel(providerType, modelID) ||
		isGoogleImageEgress(providerType, outputModalities, st.bodyBytes)
}

// isGoogleImageEgress reports a Google AI Studio candidate that has to speak
// the native dialect. Google's OpenAI-compatibility layer produces images
// only on its own /images/generations route: a chat request to an image
// model there is answered with a 400 whether or not it names an image
// modality, while the native generateContent route serves it and returns the
// image as an inlineData part. The model's discovered output modalities
// decide. A request naming an image modality is a second trigger only for a
// model whose modalities discovery left empty: a model declared text-only
// stays on the compat route, since a client could otherwise steer it onto
// the native one and have Google's refusal read as the model's retirement.
func isGoogleImageEgress(providerType, outputModalities string, body []byte) bool {
	if providerType != "google" {
		return false
	}
	declared := declaredModalities(outputModalities)
	if slices.Contains(declared, "image") {
		return true
	}
	return len(declared) == 0 && gemini.RequestWantsImage(body)
}

// isZenGeminiModel reports whether this is an OpenCode Zen candidate for a
// Gemini model. Zen's Gemini IDs are unprefixed and unsuffixed
// (gemini-3-flash, gemini-3.1-pro, gemini-3.5-flash, gemini-3.5-flash-lite,
// gemini-3.6-flash), so the family prefix is the whole test.
func isZenGeminiModel(providerType, modelID string) bool {
	return providerType == "opencode-zen" && strings.HasPrefix(strings.ToLower(modelID), "gemini-")
}

// geminiEgressEndpoint builds the native-dialect path for a provider. Vertex
// express keys only work on the publisher routes; Zen exposes the same
// generateContent verbs directly under its own /models/{id}.
func geminiEgressEndpoint(providerType, model string, stream bool) string {
	prefix := "/publishers/google/models/"
	if providerType == "opencode-zen" || providerType == "google" {
		prefix = "/models/"
	}
	if stream {
		return prefix + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
	}
	return prefix + url.PathEscape(model) + ":generateContent"
}

// setGeminiEgressAuth applies the auth scheme the native Google routes require.
// All three reject Bearer here: Vertex reads it as an OAuth2 expectation, and
// Zen's Gemini passthrough answers a Bearer token with
// {"type":"AuthError","message":"Missing API key."}. Only x-goog-api-key works,
// which is why this cannot go through SetProviderAuthHeaders for Zen or
// Google AI Studio: that switches on provider type alone, and Zen's other
// families and Google's compat layer do need Bearer.
func setGeminiEgressAuth(req *http.Request, providerType, apiKey string) {
	if providerType == "opencode-zen" || providerType == "google" {
		if apiKey != "" {
			req.Header.Set("x-goog-api-key", apiKey)
		}
		return
	}
	util.SetProviderAuthHeaders(req, providerType, apiKey)
}

// buildGeminiRequest builds the upstream request for a gemini egress attempt.
// The chat body goes through the shared rewrite path first (model rename,
// learned strips; isStreaming=false, since Gemini has no stream_options knob to
// inject), then chat -> generateContent translation. The model string the
// translation returns picks the :generateContent or :streamGenerateContent
// route.
func (h *Handler) buildGeminiRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	cleaned := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, false, &h.deprecationCache, &h.paramRenameCache, nil, learnedScopeFor(candidate))
	body, model, stream, err := gemini.TranslateRequest(cleaned)
	if err != nil {
		return nil, providerType, "", err
	}

	endpoint := geminiEgressEndpoint(providerType, model, stream)
	baseURL := candidate.provider.BaseURL
	if providerType == "google" {
		// The provider is configured with the /v1beta/openai compat base;
		// the native routes live one segment up.
		baseURL = provider.GoogleNativeBaseURL(baseURL)
	}
	targetURL := util.BuildProviderTargetURL(baseURL, providerType, endpoint)
	debuglog.Info("proxy: routing via gemini egress adapter", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "stream", stream)

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	setGeminiEgressAuth(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}
