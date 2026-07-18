package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// vertexProbeConcurrency bounds parallel countTokens probes so a long
// candidate list cannot burst-hammer the API.
const vertexProbeConcurrency = 4

// discoverVertexExpress discovers models for a Vertex AI express-mode key.
// There is no listing route an express key can call, so each shipped
// candidate (catalogs/vertex_express.json) is validated with a free
// :countTokens probe: 200 means the key can invoke the model, 404 means the
// model is not express-eligible (or retired) and it is dropped silently.
// Any auth-shaped failure (401/403) aborts discovery so a bad key reads as
// an error, not as "zero models".
func (d *DiscoveryService) discoverVertexExpress(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	if u, err := url.Parse(strings.TrimSpace(provider.BaseURL)); err != nil || u.Host == "" {
		return nil, fmt.Errorf("vertex-express: invalid base URL for provider %s", provider.Name)
	}

	candidates := GetVertexExpressCandidates()
	statuses := make([]int, len(candidates))
	errs := make([]error, len(candidates))

	var wg sync.WaitGroup
	sem := make(chan struct{}, vertexProbeConcurrency)
	for i, id := range candidates {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			statuses[i], errs[i] = d.vertexCountTokensProbe(ctx, provider.BaseURL, id, apiKey)
		})
	}
	wg.Wait()

	live := make([]*model.Model, 0, len(candidates))
	for i, id := range candidates {
		switch {
		case errs[i] != nil:
			debuglog.Error("discovery: vertex-express probe failed", "model", id, "provider", provider.Name, "provider_id", provider.ID, "error", errs[i])
			return nil, fmt.Errorf("vertex-express: probe for %s failed for provider %s: %w", id, provider.Name, errs[i])
		case statuses[i] == http.StatusUnauthorized || statuses[i] == http.StatusForbidden:
			debuglog.Error("discovery: vertex-express unauthorized", "model", id, "status", statuses[i], "provider", provider.Name, "provider_id", provider.ID)
			return nil, fmt.Errorf("vertex-express: unauthorized (HTTP %d) for provider %s — check the API key", statuses[i], provider.Name)
		case statuses[i] == http.StatusOK:
			live = append(live, liveModelStub(id, "google", provider.ID))
		default:
			debuglog.Debug("discovery: vertex-express candidate not eligible", "model", id, "status", statuses[i], "provider", provider.Name)
		}
	}

	if len(live) == 0 {
		debuglog.Warn("discovery: vertex-express found no eligible models — the key may be for a project without Vertex AI access", "provider", provider.Name, "provider_id", provider.ID, "candidates", len(candidates))
		return live, nil
	}

	debuglog.Info("discovery: vertex-express discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "candidates", len(candidates))
	return live, nil
}

// vertexCountTokensProbe issues one countTokens call for a candidate model and
// returns the HTTP status. The response body is drained and discarded — only
// reachability matters.
func (d *DiscoveryService) vertexCountTokensProbe(ctx context.Context, baseURL, modelID, apiKey string) (int, error) {
	endpoint := "/publishers/google/models/" + url.PathEscape(modelID) + ":countTokens"
	probeURL := util.BuildProviderTargetURL(baseURL, "vertex-express", endpoint)
	body := strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)

	req, err := http.NewRequestWithContext(ctx, "POST", probeURL, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
