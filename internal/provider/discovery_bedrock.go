package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// discoverBedrock discovers models from AWS Bedrock's OpenAI-optimized
// bedrock-mantle.{region}.api.aws endpoint, authenticated with a Bedrock API
// key as a bearer token. (The classic bedrock-runtime endpoint exposes no
// /models listing, so mantle is the only Bedrock surface MH can discover.)
//
// The listing is OpenAI-shaped, but the catalog mixes dialects: anthropic.*
// models reject /v1/chat/completions and /v1/responses (they are served only
// through Bedrock's Anthropic Messages endpoint at {base}/anthropic/v1/messages,
// which the proxy does not forward to yet), so exposing them would give users
// models that 400 on every request. They are skipped until a Messages
// passthrough exists. The listing carries no dialect field, so the anthropic.
// ID prefix is the classifier.
func (d *DiscoveryService) discoverBedrock(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: bedrock fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("bedrock: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: bedrock json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("bedrock: failed to decode response for provider %s: %w", provider.Name, err)
	}

	live := make([]*model.Model, 0, len(openAIResp.Data))
	skipped := 0
	for _, m := range openAIResp.Data {
		if isBedrockMessagesDialectModel(m.ID) {
			skipped++
			debuglog.Debug("discovery: bedrock skipping messages-dialect model", "model", m.ID, "provider", provider.Name)
			continue
		}
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}

	// An empty 200 usually means a misconfigured region or a revoked key, not
	// a genuinely empty catalog — surface it instead of silently succeeding.
	if len(live) == 0 {
		debuglog.Warn("discovery: bedrock /models returned no usable models, skipping", "provider", provider.Name, "provider_id", provider.ID, "skipped_messages_dialect", skipped)
		return live, nil
	}

	// No catalog for Bedrock: the live listing is authoritative and models.dev
	// backfills pricing/context/modalities (its LookupFuzzy strips the vendor.
	// prefix Bedrock puts on every ID).
	debuglog.Info("discovery: bedrock discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "skipped_messages_dialect", skipped)
	return live, nil
}

// isBedrockMessagesDialectModel reports whether a Bedrock model ID belongs to
// the Anthropic Messages dialect rather than OpenAI chat completions.
func isBedrockMessagesDialectModel(modelID string) bool {
	id := strings.ToLower(modelID)
	// Defensive: cross-region inference-profile prefixes (us./eu./apac./
	// global.) can wrap the vendor prefix in Bedrock IDs; mantle lists bare
	// IDs today, but a prefixed anthropic model is messages-dialect either way.
	for _, p := range []string{"us.", "eu.", "apac.", "global."} {
		id = strings.TrimPrefix(id, p)
	}
	return strings.HasPrefix(id, "anthropic.")
}
