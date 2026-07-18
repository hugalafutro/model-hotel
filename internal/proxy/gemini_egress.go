package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The vertex-express egress adapter. Vertex AI express-mode API keys only work
// on Google's native publisher routes, so a chat-completions request bound for
// a vertex-express provider is translated to generateContent shape on the way
// out (internal/gemini) and the response translated back on the upstream side
// of the pipeline — the same trick as the /v1/responses re-route, so the TTFT
// probe, stall watchdog, transforms and metering all run unchanged.

// isGeminiEgressAttempt reports whether this candidate is served through the
// gemini egress adapter: a plain chat-completions request (no explicit
// endpoint override, no multipart body) to a vertex-express provider.
func isGeminiEgressAttempt(st *requestState, providerType string) bool {
	return providerType == "vertex-express" && st.endpointPath == "" && st.makeUpstreamBody == nil
}

// buildGeminiRequest builds the upstream request for a vertex-express attempt.
// The chat body goes through the shared rewrite path first (model rename,
// learned strips; isStreaming=false so no stream_options is injected — Gemini
// has no such knob), then chat -> generateContent translation. The model
// string returned by the translation picks the :generateContent or
// :streamGenerateContent route.
func (h *Handler) buildGeminiRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	cleaned := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, false, &h.deprecationCache, &h.paramRenameCache, nil)
	body, model, stream, err := gemini.TranslateRequest(cleaned)
	if err != nil {
		return nil, providerType, "", err
	}

	endpoint := "/publishers/google/models/" + url.PathEscape(model) + ":generateContent"
	if stream {
		endpoint = "/publishers/google/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
	}
	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, endpoint)
	debuglog.Info("proxy: routing via gemini egress adapter", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "stream", stream)

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// translateGeminiResponseBody swaps a non-streaming generateContent 200 body
// for its chat.completion translation so handleNonStreamingResponse can meter
// and forward it unchanged.
func translateGeminiResponseBody(resp *http.Response, model string) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	translated, err := gemini.BuildChatCompletion(body, id, model, time.Now().Unix())
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(translated))
	return nil
}
