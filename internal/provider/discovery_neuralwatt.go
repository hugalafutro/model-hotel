package provider

import (
	"context"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// GetNeuralWattQuota retrieves the quota/balance from NeuralWatt.
func (d *DiscoveryService) GetNeuralWattQuota(ctx context.Context, provider *Provider, masterKey string) (*NeuralWattQuotaResponse, error) {
	var quota NeuralWattQuotaResponse
	err := d.fetchQuotaJSON(ctx, provider, masterKey, "/quota", "neuralwatt", "quota", &quota)
	// 404 = free tier key, no quota endpoint. Report no data and no error.
	if errorStatusCode(err) == http.StatusNotFound {
		debuglog.Info("discovery: neuralwatt quota endpoint not available (likely free tier)", "provider", provider.Name, "provider_id", provider.ID)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &quota, nil
}
