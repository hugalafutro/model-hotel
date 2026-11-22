package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverAnthropic(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeAPIURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("x-api-key", apiKey)
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("Content-Type", "application/json")

	var allModels []AnthropicModelInfo
	afterID := ""
	cursors := pageCursors{}

	for {
		pageURL := baseURL + "/v1/models?limit=100"
		if afterID != "" {
			pageURL += "&after_id=" + url.QueryEscape(afterID)
		}

		bodyBytes, err := d.fetchURL(ctx, "GET", pageURL, headers)
		if err != nil {
			debuglog.Error("discovery: anthropic fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		var pageResp AnthropicModelsResponse
		if err := json.Unmarshal(bodyBytes, &pageResp); err != nil {
			debuglog.Error("discovery: anthropic json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", jsonfault.Describe(err, len(bodyBytes)))
			return nil, fmt.Errorf("failed to decode response: %s", jsonfault.Describe(err, len(bodyBytes)))
		}

		allModels = append(allModels, pageResp.Data...)

		// has_more alone ends the walk. An empty page that still reports more
		// goes through the cursor check like any other, so it cannot pass for
		// the end of the listing.
		if !pageResp.HasMore {
			break
		}
		if err := cursors.advance(pageResp.LastID); err != nil {
			debuglog.Error("discovery: anthropic pagination failed", "provider", provider.Name, "provider_id", provider.ID, "pages", len(cursors)+1, "cursor", cursorForLog(pageResp.LastID), "error", err)
			return nil, fmt.Errorf("failed to page models: %w", err)
		}
		afterID = pageResp.LastID
	}

	models := make([]*model.Model, 0, len(allModels))
	for _, m := range allModels {
		displayName := m.ID
		if m.DisplayName != "" {
			displayName = m.DisplayName
		}

		caps := model.Capability{
			Streaming:   true,
			ToolCalling: true,
		}
		inputMods := `["text"]`

		if m.Capabilities != nil {
			if m.Capabilities.ImageInput.Supported {
				caps.Vision = true
				inputMods = `["text","image"]`
			}
			if m.Capabilities.PDFInput.Supported {
				caps.PDFUpload = true
				inputMods = `["text","image","pdf"]`
			}
			if m.Capabilities.StructuredOutputs.Supported {
				caps.StructuredOutput = true
			}
		}

		capJSON, _ := json.Marshal(caps)

		modelEntry := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      displayName,
			Capabilities:     string(capJSON),
			Params:           "{}",
			InputModalities:  inputMods,
			OutputModalities: `["text"]`,
			ContextLength:    m.MaxInputTokens,
			MaxOutputTokens:  m.MaxTokens,
			// Both provider types served by this discoverer report the same owner.
			// An anthropic-messages endpoint may front someone else's models, but
			// the Anthropic listing carries no ownership field to learn the truth
			// from, and leaving this empty is worse than a wrong-but-consistent
			// value: models.dev enrichment then fills OwnedBy from the model
			// FAMILY ("claude-opus"), so the same model would report a different
			// owner depending on which provider discovered it.
			OwnedBy: "anthropic",
			Enabled: true,
		}

		// Anthropic's listing carries no prices; models.dev enrichment fills
		// them, matching dated ids to their undated family row.
		models = append(models, modelEntry)
	}

	debuglog.Info("discovery: anthropic discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}
