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
		return nil, fmt.Errorf("ollama: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		debuglog.Error("discovery: ollama http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("ollama: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: ollama unexpected status", "provider", provider.Name, "provider_id", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("ollama: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var tagsResp OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		debuglog.Error("discovery: ollama json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("ollama: failed to decode response for provider %s: %w", provider.Name, err)
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
			// Without this, a panic in the show-model fetch would crash the whole
			// server instead of just skipping the one model. Record it as a
			// per-model error so the row is dropped gracefully (see r.err below).
			defer func() {
				if rec := recover(); rec != nil {
					debuglog.Error("discovery: ollama show-model goroutine panicked",
						"provider", provider.Name, "provider_id", provider.ID, "model", modelName, "panic", rec)
					results[idx] = showResult{index: idx, modelID: modelName, err: fmt.Errorf("panic: %v", rec)}
				}
			}()
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
		show := r.show
		if r.err != nil {
			// The model IS listed by /api/tags; a failed detail probe must not
			// drop it from the results, or RecordMissingModels would disable a
			// model that merely had a flaky metadata fetch. Emit it with an
			// empty show response: capabilities default to streaming-only and
			// context length stays nil (fill-only, preserved at upsert).
			debuglog.Warn("discovery: ollama show model failed, keeping model with default metadata", "provider", provider.Name, "provider_id", provider.ID, "model", r.modelID, "error", r.err)
			skipped++
			show = &OllamaShowResponse{}
		}

		m := d.buildOllamaModel(provider, r.modelID, show)
		models = append(models, m)
	}

	if skipped > 0 {
		debuglog.Info("discovery: ollama discovered models with skips", "provider", provider.Name, "provider_id", provider.ID, "models", len(models), "skipped", skipped)
	} else {
		debuglog.Info("discovery: ollama discovered models", "provider", provider.Name, "provider_id", provider.ID, "models", len(models))
	}

	// Context length comes from the live /api/show probe, so mark it live: a
	// model that gains a larger context window propagates and is reported
	// (prices are nil for Ollama, so MarkLiveMetaFromCurrent leaves them fill-only).
	markLiveMeta(models)

	return models, nil
}

func (d *DiscoveryService) ollamaShowModel(ctx context.Context, apiBase, apiKey, modelName string) (*OllamaShowResponse, error) {
	showURL := apiBase + "/api/show"
	body := fmt.Sprintf(`{"model":%q}`, modelName)

	// Rebuild the request per attempt so the POST body replays on retry.
	resp, err := d.doDiscoveryRequest(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", showURL, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
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
	outputMods := "[]"

	hasCompletion, hasEmbedding := false, false
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
		case "completion":
			hasCompletion = true
		case "embedding":
			hasEmbedding = true
		}
	}
	capJSON, _ := json.Marshal(caps)

	// Ollama reports capabilities authoritatively: an embedding-only model lists
	// "embedding" and not "completion", so it must be hidden from the chat
	// picker. Applies equally to local Ollama and Ollama Cloud (same code path).
	// If a model reports no completion capability at all (older Ollama returns an
	// empty list), fall back to a name heuristic so embed/rerank models are still
	// caught without misclassifying a normal chat model.
	if !hasCompletion && modality == "text" {
		switch {
		case hasEmbedding:
			modality = "embedding"
			_, outputMods = nonChatModalityArrays(modality)
		default:
			if inferred := inferNonChatModality(modelID); inferred != "" {
				modality = inferred
				_, outputMods = nonChatModalityArrays(inferred)
			}
		}
	}

	var contextLength *int
	for k, v := range show.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			// Guard on > 0 so a zero/absent value stays nil and isn't marked live
			// (which would let it overwrite a stored context length with 0).
			if f, ok := v.(float64); ok && f > 0 {
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
		OutputModalities: outputMods,
		ContextLength:    contextLength,
		OwnedBy:          ownedBy,
		Enabled:          true,
	}
}

// GetOllamaCloudAccount fetches the account info from the Ollama Cloud /api/me endpoint.
func (d *DiscoveryService) GetOllamaCloudAccount(ctx context.Context, provider *Provider, masterKey string) (*OllamaCloudAccount, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to decrypt API key for provider %s: %w", provider.Name, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	// Remove /v1 suffix for the account endpoint
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	accountURL := baseURL + "/api/me"

	req, err := http.NewRequestWithContext(ctx, "POST", accountURL, http.NoBody)
	// Ollama Cloud requires POST for /api/me despite being a read operation.
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to create account request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), provider.Name, "ollama-cloud")
	if err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to fetch account for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if authErr := quotaAuthError("ollama-cloud", provider, resp.StatusCode, body); authErr != nil {
			return nil, authErr
		}
		debuglog.Error("discovery: ollama cloud account non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("ollama-cloud: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var account OllamaCloudAccount
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("ollama-cloud: failed to decode account response for provider %s: %w", provider.Name, err)
	}

	debuglog.Info("discovery: ollama cloud account fetched", "provider", provider.Name, "provider_id", provider.ID, "plan", account.Plan)
	return &account, nil
}
