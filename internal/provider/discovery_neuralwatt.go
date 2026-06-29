package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// GetNeuralWattQuota retrieves the quota/balance from NeuralWatt.
func (d *DiscoveryService) GetNeuralWattQuota(ctx context.Context, provider *Provider, masterKey string) (*NeuralWattQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("neuralwatt: failed to decrypt API key for provider %s: %w", provider.Name, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	quotaURL := baseURL + "/quota"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("neuralwatt: failed to create request for provider %s: %w", provider.Name, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), provider.Name, "neuralwatt")
	if err != nil {
		return nil, fmt.Errorf("neuralwatt: failed to fetch quota for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 404 = free tier key, no quota endpoint. Return nil, nil (no data, no error).
	if resp.StatusCode == http.StatusNotFound {
		debuglog.Info("discovery: neuralwatt quota endpoint not available (likely free tier)", "provider", provider.Name, "provider_id", provider.ID)
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if authErr := quotaAuthError("neuralwatt", provider, resp.StatusCode, body); authErr != nil {
			return nil, authErr
		}
		debuglog.Error("discovery: neuralwatt quota non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("neuralwatt: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var quota NeuralWattQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		return nil, fmt.Errorf("neuralwatt: failed to decode quota response for provider %s: %w", provider.Name, err)
	}

	return &quota, nil
}
