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
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/util"
)

type DiscoveryService struct {
	httpClient *http.Client
}

func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	if strings.Contains(provider.BaseURL, "nano-gpt.com") {
		return d.discoverNanoGPT(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "z.ai") {
		return d.discoverZAI(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "deepseek.com") {
		return d.discoverDeepSeek(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "ollama.com") {
		return d.discoverOllama(ctx, provider, apiKey)
	}

	return d.discoverOpenAI(ctx, provider, apiKey)
}

func (d *DiscoveryService) discoverOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		capJSON, _ := json.Marshal(model.Capability{Streaming: true})

		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      m.ID,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         "text",
			InputModalities:  "[]",
			OutputModalities: "[]",
			OwnedBy:          m.OwnedBy,
			Enabled:          true,
		})
	}

	return models, nil
}

func (d *DiscoveryService) discoverNanoGPT(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models?detailed=true", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var nanoResp NanoGPTDetailedResponse
	if err := json.NewDecoder(resp.Body).Decode(&nanoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(nanoResp.Data))
	for _, m := range nanoResp.Data {
		caps := model.Capability{
			Streaming:         true,
			Vision:            m.Capabilities.Vision,
			VideoInput:        m.Capabilities.VideoInput,
			AudioInput:        m.Capabilities.AudioInput,
			Reasoning:         m.Capabilities.Reasoning,
			ToolCalling:       m.Capabilities.ToolCalling,
			ParallelToolCalls: m.Capabilities.ParallelToolCalls,
			StructuredOutput:  m.Capabilities.StructuredOutput,
			PDFUpload:         m.Capabilities.PDFUpload,
		}
		capJSON, _ := json.Marshal(caps)

		inputModJSON, _ := json.Marshal(m.Architecture.InputModalities)
		outputModJSON, _ := json.Marshal(m.Architecture.OutputModalities)

		displayName := m.Name
		if displayName == "" {
			displayName = m.ID
		}

		paramsMap := map[string]interface{}{}
		if m.Subscription != nil {
			paramsMap["subscription_included"] = m.Subscription.Included
			paramsMap["subscription_note"] = m.Subscription.Note
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		var inPricePerMill *float64
		var outPricePerMill *float64
		{
			v := m.Pricing.Prompt
			inPricePerMill = &v
		}
		{
			v := m.Pricing.Completion
			outPricePerMill = &v
		}

		models = append(models, &model.Model{
			ID:                    uuid.New(),
			ProviderID:            provider.ID,
			ModelID:               m.ID,
			Name:                  m.Name,
			Description:           m.Description,
			DisplayName:           displayName,
			Capabilities:          string(capJSON),
			Params:                string(paramsJSON),
			Modality:              m.Architecture.Modality,
			InputModalities:       string(inputModJSON),
			OutputModalities:      string(outputModJSON),
			ContextLength:         m.ContextLength,
			MaxOutputTokens:       m.MaxOutputTokens,
			InputPricePerMillion:  inPricePerMill,
			OutputPricePerMillion: outPricePerMill,
			OwnedBy:               m.OwnedBy,
			Enabled:               true,
		})
	}

	return models, nil
}

func (d *DiscoveryService) GetNanoGPTUsage(ctx context.Context, provider *Provider, masterKey string) (*NanoGPTUsageResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	usageURL := baseURL + "/usage"

	req, err := http.NewRequestWithContext(ctx, "GET", usageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch usage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var usage NanoGPTUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &usage, nil
}

func (d *DiscoveryService) discoverZAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalog := GetZAIModels()

	type testResult struct {
		index   int
		available bool
	}

	results := make([]testResult, len(catalog))
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup

	testCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	for i, spec := range catalog {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, modelID string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = testResult{index: idx, available: d.testZAIModel(testCtx, provider, apiKey, modelID)}
		}(i, spec.ModelID)
	}
	wg.Wait()

	models := make([]*model.Model, 0, len(catalog))
	for _, r := range results {
		if !r.available {
			continue
		}
		spec := catalog[r.index]

		contextLen := spec.ContextLength
		maxOutput := spec.MaxOutputTokens

		inputMods := `"text"`
		if spec.Modality == "vision" {
			inputMods = `["text","image","video","file"]`
		}

		caps := model.Capability{
			Streaming:        true,
			Reasoning:        spec.Reasoning,
			ToolCalling:      spec.ToolCalling,
			StructuredOutput: spec.StructuredOutput,
		}
		if spec.Modality == "vision" {
			caps.Vision = true
			caps.VideoInput = true
		}
		capJSON, _ := json.Marshal(caps)

		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          spec.ModelID,
			Name:             spec.ModelID,
			DisplayName:      spec.ModelID,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         spec.Modality,
			InputModalities:  inputMods,
			OutputModalities: "[]",
			ContextLength:    &contextLen,
			MaxOutputTokens:  &maxOutput,
			OwnedBy:          "zhipu",
			Enabled:          true,
		})
	}

	return models, nil
}

func (d *DiscoveryService) testZAIModel(ctx context.Context, provider *Provider, apiKey, modelID string) bool {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	reqBody := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1,"stream":false}`, modelID)

	client := &http.Client{Timeout: 20 * time.Second}

	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Duration(attempt) * 3 * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", strings.NewReader(reqBody))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 {
			continue
		}
		return resp.StatusCode < 400
	}
	return false
}

func (d *DiscoveryService) discoverDeepSeek(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetDeepSeekModels()
	catalogMap := make(map[string]DeepSeekModelSpec)
	for _, spec := range catalog {
		catalogMap[spec.ModelID] = spec
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec, ok := catalogMap[m.ID]
		if !ok {
			continue
		}

		contextLen := spec.ContextLength
		maxOutput := spec.MaxOutputTokens

		caps := model.Capability{
			Streaming:   true,
			Reasoning:   spec.Reasoning,
			ToolCalling: true,
		}
		capJSON, _ := json.Marshal(caps)

		inPricePerMill := spec.InputPricePerMillionCacheMiss
		outPricePerMill := spec.OutputPricePerMillion
		inPriceCacheHit := spec.InputPricePerMillionCacheHit

		models = append(models, &model.Model{
			ID:                           uuid.New(),
			ProviderID:                   provider.ID,
			ModelID:                      m.ID,
			Name:                         m.ID,
			DisplayName:                  m.ID,
			Capabilities:                 string(capJSON),
			Params:                       "{}",
			Modality:                     "text",
			InputModalities:              "[]",
			OutputModalities:             "[]",
			ContextLength:                &contextLen,
			MaxOutputTokens:              &maxOutput,
			InputPricePerMillion:        &inPricePerMill,
			InputPricePerMillionCacheHit: &inPriceCacheHit,
			OutputPricePerMillion:       &outPricePerMill,
			OwnedBy:                      m.OwnedBy,
			Enabled:                      true,
		})
	}

	return models, nil
}

func (d *DiscoveryService) GetDeepSeekBalance(ctx context.Context, provider *Provider, masterKey string) (*DeepSeekBalanceResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	balanceURL := baseURL + "/user/balance"

	req, err := http.NewRequestWithContext(ctx, "GET", balanceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var balance DeepSeekBalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&balance); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &balance, nil
}

func (d *DiscoveryService) discoverOllama(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	apiBase := strings.TrimSuffix(strings.TrimSuffix(baseURL, "/"), "/v1")

	tagsURL := apiBase + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, "GET", tagsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var tagsResp OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
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
	for _, r := range results {
		if r.err != nil {
			continue
		}

		m := d.buildOllamaModel(provider, r.modelID, r.show)
		models = append(models, m)
	}

	return models, nil
}

func (d *DiscoveryService) ollamaShowModel(ctx context.Context, apiBase, apiKey, modelName string) (*OllamaShowResponse, error) {
	showURL := apiBase + "/api/show"
	body := fmt.Sprintf(`{"model":"%s"}`, modelName)

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("show failed for %s: status %d: %s", modelName, resp.StatusCode, string(respBody))
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