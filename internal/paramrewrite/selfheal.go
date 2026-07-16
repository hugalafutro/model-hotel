package paramrewrite

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// SelfHealChatCompletion sends a non-streaming chat-completions request and,
// when the upstream answers 400 with an error that names rejected or renamed
// params, rebuilds the body through BuildUpstreamBody (applying the learned
// rejects/renames) and retries once. It is the standalone, single-candidate
// analog of the proxy failover loop's retryWithStrippedParams: both learn the
// same way (ParseProviderParamError / ParseProviderParamRename) and rewrite
// through the same BuildUpstreamBody, so a probe (e.g. the admin "Test model"
// button) heals the exact same 400s live traffic does — most importantly the
// OpenAI gpt-5/o-series max_tokens -> max_completion_tokens rename.
//
// baseBody is the OpenAI-shaped request body. providerType/modelID drive the
// rewrite. applyHeaders is invoked on every request to (re)apply auth and
// content-type headers, since each attempt uses a fresh *http.Request.
//
// On success (or a non-param 400, or a transport error) it returns the response
// with its Body still readable; the caller owns closing it. When a retry is
// issued the first 400 response body is drained and closed internally.
func SelfHealChatCompletion(
	ctx context.Context,
	client *http.Client,
	targetURL string,
	providerType string,
	modelID string,
	baseBody []byte,
	applyHeaders func(*http.Request),
) (*http.Response, error) {
	// Per-call learned caches. A probe is one-shot, so these start empty and
	// only carry a rename learned from this call's own first-attempt 400 into
	// its retry — no cross-request state, unlike the proxy's Handler-scoped caches.
	var deprecationCache, renameCache sync.Map

	body := BuildUpstreamBody(baseBody, providerType, modelID, modelID, false, &deprecationCache, &renameCache, nil)
	resp, err := postChatCompletion(ctx, client, targetURL, body, applyHeaders)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		return resp, err
	}

	// 400: read the error body so we can learn from it, then decide whether to
	// retry. A read error only leaves errBody empty/partial, which the parsers
	// below treat as "not a param error" — so we fall through and hand the 400
	// back rather than masking it.
	errBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	rejected := ParseProviderParamError(errBody)
	renames := ParseProviderParamRename(errBody)
	if rejected == nil && renames == nil {
		// Not a param error we know how to heal — hand back the original 400
		// with its body restored so the caller can surface the upstream message.
		resp.Body = io.NopCloser(bytes.NewReader(errBody))
		return resp, nil
	}

	if renames != nil {
		MergeLearnedParamCache(&renameCache, providerType+":"+modelID, renames)
	}
	debuglog.Debug("paramrewrite: self-heal retry", "provider_type", providerType, "model", modelID, "rejected", rejected, "renames", renames)

	rebuilt := BuildUpstreamBody(baseBody, providerType, modelID, modelID, false, &deprecationCache, &renameCache, rejected)
	return postChatCompletion(ctx, client, targetURL, rebuilt, applyHeaders)
}

// postChatCompletion builds and sends a single POST with the given body,
// applying caller-supplied headers.
func postChatCompletion(ctx context.Context, client *http.Client, targetURL string, body []byte, applyHeaders func(*http.Request)) (*http.Response, error) {
	//nolint:gosec // targetURL is admin-configured provider base, not user input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if applyHeaders != nil {
		applyHeaders(req)
	}
	return client.Do(req)
}
