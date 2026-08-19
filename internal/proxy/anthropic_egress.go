package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The Anthropic egress adapter. Anthropic's OpenAI-compat
// /v1/chat/completions cannot express a document: an OpenAI {"type":"file"}
// content part is rejected with `messages.0.user.content.str: Input should be
// a valid string`. Such a request is translated to Anthropic's native
// /v1/messages shape on the way out (internal/anthropicegress) and the answer
// translated back on the upstream side of the pipeline — the same trick as the
// gemini egress adapter and the /v1/responses re-route, so the TTFT probe,
// stall watchdog, transforms and metering all run unchanged.

// isAnthropicEgressAttempt reports whether this candidate is served through the
// Anthropic egress adapter: a plain chat-completions request (no explicit
// endpoint override, no multipart body) bound for an Anthropic provider and
// carrying a content part the compat endpoint cannot express.
//
// An Anthropic-in request is excluded: it already has a better path
// (buildNativeAnthropicRequest forwards its original Messages body verbatim),
// and translating a body that is already Anthropic-shaped would corrupt it.
func isAnthropicEgressAttempt(st *requestState, providerType string) bool {
	if providerType != "anthropic" || st.anthropicIn {
		return false
	}
	if st.endpointPath != "" || st.makeUpstreamBody != nil {
		return false
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
	cleaned := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, false, &h.deprecationCache, &h.paramRenameCache, nil, learnedScopeFor(candidate))
	body, model, stream, err := anthropicegress.TranslateRequest(cleaned)
	if err != nil {
		return nil, providerType, "", err
	}

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

// translateAnthropicEgressResponseBody swaps a non-streaming Messages 200 body
// for its chat.completion translation so handleNonStreamingResponse can meter
// and forward it unchanged.
func translateAnthropicEgressResponseBody(resp *http.Response, model string) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	translated, err := anthropicegress.BuildChatCompletion(body, id, model, time.Now().Unix())
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(translated))
	return nil
}
