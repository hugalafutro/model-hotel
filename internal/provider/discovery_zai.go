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
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/util"
)

func (d *DiscoveryService) discoverZAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalog := GetZAIModels()

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
