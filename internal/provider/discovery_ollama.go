package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOllama(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	apiBase := util.SanitizeAPIURL(provider.BaseURL)

	tagsURL := apiBase + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, "GET", tagsURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request for provider %s: %w", provider.ID, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: ollama http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("ollama: failed to fetch models for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: ollama unexpected status", "provider", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("ollama: unexpected status code %d for provider %s", resp.StatusCode, provider.ID)
	}

	var tagsResp OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		debuglog.Error("discovery: ollama json decode failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("ollama: failed to decode response for provider %s: %w", provider.ID, err)
	}

	type showResult struct {
		index   int
		modelID string
		show    *OllamaShowResponse
		err     error
	}

	results := make([]showResult, len(tagsResp.Models))
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	showCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	for i, m := range tagsResp.Models {
		wg.Add(1)
		go func(idx int, modelName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			show, err := d.ollamaShowModel(showCtx, apiBase, apiKey, modelName)
			results[idx] = showResult{index: idx, modelID: modelName, show: show, err: err}
		}(i, m.Name)
	}
	wg.Wait()

	models := make([]*model.Model, 0, len(tagsResp.Models))
	skipped := 0
	for _, r := range results {
		if r.err != nil {
			debuglog.Warn("discovery: ollama show model failed", "provider", provider.ID, "model", r.modelID, "error", r.err)
			skipped++
			continue
		}

		m := d.buildOllamaModel(provider, r.modelID, r.show)
		models = append(models, m)
	}

	if skipped > 0 {
		debuglog.Info("discovery: ollama discovered models with skips", "provider", provider.ID, "models", len(models), "skipped", skipped)
	} else {
		debuglog.Info("discovery: ollama discovered models", "provider", provider.ID, "models", len(models))
	}

	return models, nil
}

func (d *DiscoveryService) ollamaShowModel(ctx context.Context, apiBase, apiKey, modelName string) (*OllamaShowResponse, error) {
	showURL := apiBase + "/api/show"
	body := fmt.Sprintf(`{"model":%q}`, modelName)

	req, err := http.NewRequestWithContext(ctx, "POST", showURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: ollama show model failed with status", "model", modelName, "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("show failed for %s: status %d", modelName, resp.StatusCode)
	}

	var showResp OllamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&showResp); err != nil {
		return nil, err
	}
	return &showResp, nil
}

func (d *DiscoveryService) buildOllamaModel(provider *Provider, modelID string, show *OllamaShowResponse) *model.Model {
	caps := model.Capability{Streaming: true}
	modality := "text"
	inputMods := `["text"]`

	for _, c := range show.Capabilities {
		switch c {
		case "tools":
			caps.ToolCalling = true
		case "thinking":
			caps.Reasoning = true
		case "vision":
			caps.Vision = true
			modality = "vision"
			inputMods = `["text","image"]`
		}
	}
	capJSON, _ := json.Marshal(caps)

	var contextLength *int
	for k, v := range show.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if f, ok := v.(float64); ok {
				cl := int(f)
				contextLength = &cl
				break
			}
		}
	}

	ownedBy := show.Details.Family
	if ownedBy == "" {
		ownedBy = "ollama"
	}

	return &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider.ID,
		ModelID:          modelID,
		Name:             modelID,
		DisplayName:      modelID,
		Capabilities:     string(capJSON),
		Params:           "{}",
		Modality:         modality,
		InputModalities:  inputMods,
		OutputModalities: "[]",
		ContextLength:    contextLength,
		OwnedBy:          ownedBy,
		Enabled:          true,
	}
}

// GetOllamaCloudAccount fetches the account info from the Ollama Cloud /api/me endpoint.
func (d *DiscoveryService) GetOllamaCloudAccount(ctx context.Context, provider *Provider, masterKey string) (*OllamaCloudAccount, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to decrypt API key for provider %s: %w", provider.ID, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	// Remove /v1 suffix for the account endpoint
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	accountURL := baseURL + "/api/me"

	req, err := http.NewRequestWithContext(ctx, "POST", accountURL, http.NoBody)
	// Ollama Cloud requires POST for /api/me despite being a read operation.
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to create account request for provider %s: %w", provider.ID, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "ollama-cloud")
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to fetch account for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: ollama cloud account non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("ollama-cloud: unexpected status code %d for provider %s", resp.StatusCode, provider.ID)
	}

	var account OllamaCloudAccount
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to decode account response for provider %s: %w", provider.ID, err)
	}

	debuglog.Info("discovery: ollama cloud account fetched", "provider", provider.ID, "plan", account.Plan)
	return &account, nil
}
