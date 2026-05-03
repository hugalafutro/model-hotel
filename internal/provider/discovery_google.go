package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverGoogleAIStudio(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	// Determine the native API base URL from the proxy base URL.
	// The proxy uses /v1beta/openai/ but discovery uses /v1beta/models?key=KEY
	nativeBaseURL := toNativeBaseURL(baseURL)

	// Use ?key= auth for native API
	url := fmt.Sprintf("%s/models?key=%s", nativeBaseURL, apiKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[discovery] google: http request failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[discovery] google: non-200 status %d from provider %s: %s", resp.StatusCode, provider.ID, string(bodyBytes))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var googleResp GoogleModelsResponse
	if err := json.Unmarshal(bodyBytes, &googleResp); err != nil {
		log.Printf("[discovery] google: failed to decode response from provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	pricingCatalog := GetGooglePricingCatalog()
	models := make([]*model.Model, 0, len(googleResp.Models))

	for _, gm := range googleResp.Models {
		// Strip "models/" prefix for our internal model ID
		modelID := strings.TrimPrefix(gm.Name, "models/")

		// Skip non-text/image models (video generation, embedding-only, AQA)
		if !isRelevantGoogleModel(gm) {
			log.Printf("[discovery] google: skipping non-chat model %s", modelID)
			continue
		}

		pricing := LookupGooglePricing(pricingCatalog, gm.Name)

		// Build capabilities from API data
		hasThinking := gm.Thinking
		hasGenerateContent := containsString(gm.SupportedGenerationMethods, "generateContent")

		caps := model.Capability{
			Streaming:        hasGenerateContent,
			Reasoning:        hasThinking,
			ToolCalling:      isGoogleToolCallingModel(modelID),
			StructuredOutput: isGoogleStructuredOutputModel(modelID),
			Vision:           isGoogleVisionModel(modelID),
		}
		capJSON, _ := json.Marshal(caps)

		// Determine modality from model name
		modality := "text"
		inputMods := `["text"]`
		outputMods := `["text"]`
		if isGoogleVisionModel(modelID) {
			inputMods = `["text","image"]`
		}
		if isGoogleImageGenModel(modelID) {
			modality = "text"
			outputMods = `["text","image"]`
			inputMods = `["text","image"]`
		}
		if isGoogleAudioModel(modelID) {
			inputMods = `["text","image","audio","video"]`
			outputMods = `["text","audio"]`
			modality = "text"
		}
		if isGoogleEmbeddingModel(modelID) {
			modality = "embedding"
			inputMods = `["text","image","video","audio"]`
			outputMods = `["embedding"]`
		}

		ctxLen := gm.InputTokenLimit
		maxOut := gm.OutputTokenLimit

		m := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          modelID,
			Name:             modelID,
			DisplayName:      gm.DisplayName,
			Description:      gm.Description,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         modality,
			InputModalities:  inputMods,
			OutputModalities: outputMods,
			ContextLength:    &ctxLen,
			MaxOutputTokens:  &maxOut,
			OwnedBy:          "google",
			Enabled:          true,
		}

		// Enrich with pricing from catalog
		if pricing != nil {
			m.InputPricePerMillion = &pricing.InputPricePerMillion
			m.OutputPricePerMillion = &pricing.OutputPricePerMillion
			if pricing.InputPricePerMillionCacheHit > 0 {
				m.InputPricePerMillionCacheHit = &pricing.InputPricePerMillionCacheHit
			}
		}

		models = append(models, m)
	}

	log.Printf("[discovery] google: discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}

// toNativeBaseURL converts a proxy base URL to the native API base URL.
// Proxy:  https://generativelanguage.googleapis.com/v1beta/openai
// Native: https://generativelanguage.googleapis.com/v1beta
func toNativeBaseURL(proxyURL string) string {
	u := strings.TrimSuffix(proxyURL, "/")
	if strings.HasSuffix(u, "/openai") {
		return strings.TrimSuffix(u, "/openai")
	}
	return u
}

func isRelevantGoogleModel(gm GoogleModel) bool {
	for _, method := range gm.SupportedGenerationMethods {
		if method == "generateContent" || method == "embedContent" {
			return true
		}
	}
	return false
}

func isGoogleToolCallingModel(modelID string) bool {
	excluded := []string{"embedding", "imagen", "veo", "lyria", "aqa", "tts", "live"}
	lower := strings.ToLower(modelID)
	for _, ex := range excluded {
		if strings.Contains(lower, ex) {
			return false
		}
	}
	return true
}

func isGoogleStructuredOutputModel(modelID string) bool {
	return isGoogleToolCallingModel(modelID)
}

func isGoogleVisionModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	excluded := []string{"embedding", "tts", "live"}
	for _, ex := range excluded {
		if strings.Contains(lower, ex) {
			return false
		}
	}
	return strings.Contains(lower, "gemini-2") || strings.Contains(lower, "gemini-3") || strings.Contains(lower, "gemma")
}

func isGoogleImageGenModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "image") || strings.Contains(lower, "banana")
}

func isGoogleAudioModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "tts") || strings.Contains(lower, "live") || strings.Contains(lower, "native-audio")
}

func isGoogleEmbeddingModel(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "embedding")
}

// containsString checks if a slice contains a specific string value.
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

