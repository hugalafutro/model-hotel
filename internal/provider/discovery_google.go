package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverGoogleAIStudio(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	// The proxy uses /v1beta/openai/; discovery uses the native /v1beta/models.
	nativeBaseURL := GoogleNativeBaseURL(baseURL)

	// The key goes in the x-goog-api-key header, never the URL: a transport
	// failure quotes the whole URL back through *url.Error, and Go redacts only
	// userinfo passwords, not query parameters.
	req, err := http.NewRequestWithContext(ctx, "GET", nativeBaseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("google: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		// err is already masked by the shared retry path. %s, not %w: callers
		// must not unwrap to the raw transport error.
		debuglog.Error("discovery: google http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err.Error())
		return nil, fmt.Errorf("google: failed to fetch models for provider %s: %s", provider.Name, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google: failed to read response for provider %s: %w", provider.Name, err)
	}

	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: google non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", maskRequestSecrets(req, string(bodyBytes), 2000))
		return nil, fmt.Errorf("google: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var googleResp GoogleModelsResponse
	if err := json.Unmarshal(bodyBytes, &googleResp); err != nil {
		debuglog.Error("discovery: google failed to decode response", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("google: failed to decode response for provider %s: %w", provider.Name, err)
	}

	pricingCatalog := GetGooglePricingCatalog()
	models := make([]*model.Model, 0, len(googleResp.Models))

	for _, gm := range googleResp.Models {
		// Strip "models/" prefix for our internal model ID
		modelID := strings.TrimPrefix(gm.Name, "models/")

		// Skip non-text/image models (video generation, embedding-only, AQA)
		if !isRelevantGoogleModel(gm) {
			debuglog.Info("discovery: google skipping non-chat model", "model", modelID)
			continue
		}

		// Google keeps shut-down models in its listing, so a live entry is not
		// proof the model still answers. Drop the known-retired IDs here rather
		// than upserting them as Enabled and 404ing on every request.
		if IsRetiredGoogleModel(modelID) {
			debuglog.Info("discovery: google skipping retired model", "model", modelID)
			continue
		}

		pricing := LookupGooglePricing(pricingCatalog, gm.Name)

		// Build capabilities from API data
		hasThinking := gm.Thinking
		hasGenerateContent := slices.Contains(gm.SupportedGenerationMethods, "generateContent")

		caps := model.Capability{
			Streaming:        hasGenerateContent,
			Reasoning:        hasThinking,
			ToolCalling:      isGoogleToolCallingModel(modelID),
			StructuredOutput: isGoogleStructuredOutputModel(modelID),
			Vision:           isGoogleVisionModel(modelID),
		}
		capJSON, _ := json.Marshal(caps)

		// Determine modality arrays from the model name; the endpoint class
		// is derived centrally by NormalizeModelClassification.
		inputMods, outputMods := googleModalities(modelID)

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

	debuglog.Info("discovery: google discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// GoogleNativeBaseURL converts a proxy base URL to the native API base URL;
// the proxy's gemini egress adapter uses it too.
// Proxy:  https://generativelanguage.googleapis.com/v1beta/openai
// Native: https://generativelanguage.googleapis.com/v1beta
func GoogleNativeBaseURL(proxyURL string) string {
	u := strings.TrimSuffix(proxyURL, "/")
	if before, ok := strings.CutSuffix(u, "/openai"); ok {
		return before
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
	excluded := []string{"embedding", "imagen", "veo", "lyria", "aqa", "live"}
	lower := strings.ToLower(modelID)
	for _, ex := range excluded {
		if strings.Contains(lower, ex) {
			return false
		}
	}
	return !isGoogleTTSModel(modelID)
}

// isGoogleStructuredOutputModel reports a model that honours JSON mode. An
// image-output model does not, whatever Google's docs list for it: the API
// answers responseMimeType application/json on gemini-2.5-flash-image with
// "JSON mode is not enabled for this model" and on the gemini-3 image models
// with a bare INVALID_ARGUMENT (google-gemini/cookbook#1028), for a schema
// and for plain json_object alike.
func isGoogleStructuredOutputModel(modelID string) bool {
	return isGoogleToolCallingModel(modelID) && !isGoogleImageGenModel(modelID)
}

func isGoogleVisionModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	excluded := []string{"embedding", "live"}
	for _, ex := range excluded {
		if strings.Contains(lower, ex) {
			return false
		}
	}
	if isGoogleTTSModel(modelID) {
		return false
	}
	return strings.Contains(lower, "gemini-2") || strings.Contains(lower, "gemini-3") || strings.Contains(lower, "gemma")
}

func isGoogleImageGenModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "image") || strings.Contains(lower, "banana")
}

// googleModalities derives a Google model's input and output modality arrays
// from its name; the model list carries no modality information of its own.
func googleModalities(modelID string) (inputMods, outputMods string) {
	inputMods = `["text"]`
	outputMods = `["text"]`
	if isGoogleVisionModel(modelID) {
		inputMods = `["text","image"]`
		// Gemini chat models also hear: the OpenAI-compatibility route
		// accepts an input_audio part (verified live: gemini-3.5-flash-lite
		// transcribed a wav, gemma-4-31b-it answered the same request with
		// 400, so Gemma stays image-only). PDF is left out on purpose: the
		// same route answers a file part with 400.
		if strings.Contains(strings.ToLower(modelID), "gemini-") {
			inputMods = `["text","image","audio"]`
		}
	}
	if isGoogleImageGenModel(modelID) {
		outputMods = `["text","image"]`
		inputMods = `["text","image"]`
	}
	if isGoogleAudioModel(modelID) {
		inputMods = `["text","image","audio","video"]`
		outputMods = `["text","audio"]`
	}
	if isGoogleTTSModel(modelID) {
		// A text-to-speech model speaks and does nothing else: it refuses a
		// TEXT response modality and takes no image or audio input
		// ("The requested combination of response modalities (TEXT) is not
		// supported"). Text in and audio out derives the tts class, which
		// keeps it out of the chat and arena pickers and stops /v1/models
		// advertising vision or audio input. An audio-only output also
		// exempts it from the model-gone strike. Nothing gates a request by
		// modality on the way in, so a client naming it directly still gets
		// Google's 400.
		inputMods = `["text"]`
		outputMods = `["audio"]`
	}
	if isGoogleEmbeddingModel(modelID) {
		// gemini-embedding-001 embeds text only (models.dev agrees); the
		// input list drives the vision/audio/video capability flags, so
		// anything wider advertises pills the model cannot honour.
		inputMods = `["text"]`
		outputMods = `["embedding"]`
	}
	return inputMods, outputMods
}

// isGoogleTTSModel reports a text-to-speech model (gemini-2.5-flash-preview-tts
// and kin), as opposed to the live and native-audio models that both hear and
// speak alongside text. It matches "tts" as a whole name segment so a chat
// model that merely contains the letters cannot lose its chat class.
func isGoogleTTSModel(modelID string) bool {
	return slices.Contains(util.ModelIDSegments(modelID), "tts")
}

// isGoogleAudioModel reports a model that hears and speaks alongside text; a
// text-to-speech model is not one, so the two derivations never overlap.
func isGoogleAudioModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "live") || strings.Contains(lower, "native-audio")
}

func isGoogleEmbeddingModel(modelID string) bool {
	return strings.Contains(strings.ToLower(modelID), "embedding")
}
