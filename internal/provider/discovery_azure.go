package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// azureProjectDeployment is one entry of the Foundry project data-plane
// deployments listing (GET {root}/api/projects/{proj}/deployments?api-version=v1).
type azureProjectDeployment struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	ModelName      string `json:"modelName"`
	ModelPublisher string `json:"modelPublisher"`
}

// azureLegacyDeployment is one entry of the classic data-plane deployments
// listing (GET {root}/openai/deployments?api-version=2023-03-15-preview) —
// the only api-version that still serves the listing (GA versions dropped it);
// live-verified 2026-07-18 on a Foundry resource.
type azureLegacyDeployment struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Status string `json:"status"`
}

// discoverAzure discovers models from an Azure AI Foundry or classic Azure
// OpenAI resource. Azure's /openai/v1/models returns the full Azure model
// catalog (300+ entries), but only *deployments* the user created are
// invokable — and requests must name the deployment, not the base model — so
// discovery enumerates deployments instead.
//
// Two base URL shapes are accepted:
//   - the Foundry project endpoint ({root}/api/projects/{proj}) — exactly what
//     the Foundry portal hands the user — listed via the modern project
//     data-plane route, which also carries the underlying model name/publisher;
//   - anything else on an Azure AI host (resource root or an /openai/v1 base),
//     listed via the classic deployments route.
//
// Both routes accept the resource API key as a bearer token (live-verified
// 2026-07-18). The deployment name becomes ModelID (it is the invokable
// identifier); the underlying base-model name is kept in Name so models.dev
// enrichment can match deployments whose alias differs from the model.
func (d *DiscoveryService) discoverAzure(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	u, err := url.Parse(strings.TrimSpace(provider.BaseURL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("azure: invalid base URL for provider %s", provider.Name)
	}
	root := u.Scheme + "://" + u.Host

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	var live []*model.Model
	if project := azureProjectFromPath(u.Path); project != "" {
		live, err = d.azureProjectDeployments(ctx, provider, root, project, headers)
	} else {
		live, err = d.azureLegacyDeployments(ctx, provider, root, headers)
	}
	if err != nil {
		return nil, err
	}

	// Zero deployments is a real state for a fresh resource, but it renders the
	// provider useless in MH — surface it instead of silently succeeding.
	if len(live) == 0 {
		debuglog.Warn("discovery: azure listed no model deployments — deploy a model in the Azure portal first", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	debuglog.Info("discovery: azure discovered deployments", "provider", provider.Name, "provider_id", provider.ID, "live", len(live))
	return live, nil
}

// azureProjectFromPath extracts the Foundry project name from a base URL path
// like /api/projects/{proj}. Returns "" when the path is not a project endpoint.
func azureProjectFromPath(path string) string {
	const marker = "/api/projects/"
	idx := strings.Index(strings.ToLower(path), marker)
	if idx == -1 {
		return ""
	}
	rest := path[idx+len(marker):]
	project, _, _ := strings.Cut(rest, "/")
	return project
}

// azureProjectDeployments lists deployments via the Foundry project data-plane.
func (d *DiscoveryService) azureProjectDeployments(ctx context.Context, provider *Provider, root, project string, headers http.Header) ([]*model.Model, error) {
	listURL := root + "/api/projects/" + url.PathEscape(project) + "/deployments?api-version=v1"
	bodyBytes, err := d.fetchURL(ctx, "GET", listURL, headers)
	if err != nil {
		debuglog.Error("discovery: azure fetch project deployments failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("azure: failed to fetch deployments for provider %s: %w", provider.Name, err)
	}

	var resp struct {
		Value []azureProjectDeployment `json:"value"`
	}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		debuglog.Error("discovery: azure json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("azure: failed to decode response for provider %s: %w", provider.Name, err)
	}

	live := make([]*model.Model, 0, len(resp.Value))
	for _, dep := range resp.Value {
		if dep.Type != "" && dep.Type != "ModelDeployment" {
			debuglog.Debug("discovery: azure skipping non-model deployment", "deployment", dep.Name, "type", dep.Type, "provider", provider.Name)
			continue
		}
		m := liveModelStub(dep.Name, dep.ModelPublisher, provider.ID)
		if dep.ModelName != "" {
			m.Name = dep.ModelName
		}
		live = append(live, m)
	}
	return live, nil
}

// azureLegacyDeployments lists deployments via the classic data-plane route,
// which works on both Foundry and classic resources and needs no project name.
func (d *DiscoveryService) azureLegacyDeployments(ctx context.Context, provider *Provider, root string, headers http.Header) ([]*model.Model, error) {
	listURL := root + "/openai/deployments?api-version=2023-03-15-preview"
	bodyBytes, err := d.fetchURL(ctx, "GET", listURL, headers)
	if err != nil {
		debuglog.Error("discovery: azure fetch legacy deployments failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("azure: failed to fetch deployments for provider %s: %w", provider.Name, err)
	}

	var resp struct {
		Data []azureLegacyDeployment `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		debuglog.Error("discovery: azure json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("azure: failed to decode response for provider %s: %w", provider.Name, err)
	}

	live := make([]*model.Model, 0, len(resp.Data))
	for _, dep := range resp.Data {
		if dep.Status != "" && dep.Status != "succeeded" {
			debuglog.Debug("discovery: azure skipping non-succeeded deployment", "deployment", dep.ID, "status", dep.Status, "provider", provider.Name)
			continue
		}
		m := liveModelStub(dep.ID, "", provider.ID)
		if dep.Model != "" {
			m.Name = dep.Model
		}
		live = append(live, m)
	}
	return live, nil
}
