package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverZAICoding(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalog := GetZAICodingModels()

	type testResult struct {
		index     int
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
			results[idx] = testResult{index: idx, available: d.testZAICodingModel(testCtx, provider, apiKey, modelID)}
		}(i, spec.ModelID)
	}
	wg.Wait()

	available := 0
	models := make([]*model.Model, 0, len(catalog))
	for _, r := range results {
		if !r.available {
			continue
		}
		available++
		spec := catalog[r.index]

		contextLen := spec.ContextLength
		maxOutput := spec.MaxOutputTokens

		inputMods := `["text"]`
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

	log.Printf("[discovery] zai-coding provider %s: discovered %d/%d models available", provider.ID, available, len(catalog))

	return models, nil
}

func (d *DiscoveryService) GetZAICodingQuota(ctx context.Context, provider *Provider, masterKey string) (*ZAICodingQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	quotaURL := "https://api.z.ai/api/monitor/usage/quota/limit"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "zai-coding")
	if err != nil {
		log.Printf("[discovery] error: zai-coding provider %s quota fetch failed: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch quota: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[discovery] error: zai-coding provider %s quota fetch returned status %d: %s", provider.ID, resp.StatusCode, string(body))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var quota ZAICodingQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		log.Printf("[discovery] error: zai-coding provider %s quota decode failed: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &quota, nil
}

func (d *DiscoveryService) testZAICodingModel(ctx context.Context, provider *Provider, apiKey, modelID string) bool {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	reqBody := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1,"stream":false}`, modelID)

	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				log.Printf("[discovery] warning: zai-coding model %s availability test failed: context cancelled after %d attempts", modelID, attempt)
				return false
			case <-time.After(time.Duration(attempt) * 3 * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", strings.NewReader(reqBody))
		if err != nil {
			log.Printf("[discovery] warning: zai-coding model %s availability test failed: request creation error: %v", modelID, err)
			return false
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := d.httpClient.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 429 {
			log.Printf("[discovery] zai-coding model %s rate limited (429), retrying", modelID)
			continue
		}
		return resp.StatusCode < 400
	}
	log.Printf("[discovery] warning: zai-coding model %s availability test failed after all retries", modelID)
	return false
}
