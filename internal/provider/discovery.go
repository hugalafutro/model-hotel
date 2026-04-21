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

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIModelsResponse struct {
	Object string         `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type NanoGPTArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type NanoGPTCapabilities struct {
	Vision            bool `json:"vision"`
	VideoInput       bool `json:"video_input"`
	AudioInput       bool `json:"audio_input"`
	Reasoning        bool `json:"reasoning"`
	ToolCalling      bool `json:"tool_calling"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StructuredOutput  bool `json:"structured_output"`
	PDFUpload        bool `json:"pdf_upload"`
}

type NanoGPTPricing struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
	Currency   string  `json:"currency"`
	Unit       string  `json:"unit"`
}

type NanoGPTSubscription struct {
	Included bool   `json:"included"`
	Note     string `json:"note"`
}

type NanoGPTModel struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Description     string                `json:"description"`
	ContextLength   *int                  `json:"context_length"`
	MaxOutputTokens *int                  `json:"max_output_tokens"`
	OwnedBy         string                `json:"owned_by"`
	Architecture    NanoGPTArchitecture   `json:"architecture"`
	Capabilities    NanoGPTCapabilities   `json:"capabilities"`
	Pricing         NanoGPTPricing       `json:"pricing"`
	Subscription    *NanoGPTSubscription  `json:"subscription"`
}

type NanoGPTDetailedResponse struct {
	Object string         `json:"object"`
	Data   []NanoGPTModel `json:"data"`
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