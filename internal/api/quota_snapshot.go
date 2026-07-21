package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// quotaKindFor maps a provider type to the snapshot kind it produces, or
// ok=false when the type exposes no quota/usage/balance/account endpoint.
func quotaKindFor(providerType string) (string, bool) {
	switch providerType {
	case "nanogpt", "zai-coding", "kimi-code", "minimax", "openrouter", "neuralwatt":
		return "usage", true
	case "deepseek":
		return "balance", true
	case "ollama-cloud":
		return "account", true
	default:
		return "", false
	}
}

// fetchQuotaSnapshot performs the live upstream call for a provider and returns
// the JSON body, the HTTP status the endpoint would send, and an error only for
// unexpected failures. A dead credential becomes 424; NeuralWatt free-tier
// (nil result) becomes 204 with a null payload. This is the single source of
// truth shared by the poller, manual refresh, and cold lazy-fill.
//
// Each discovery result is captured into its concrete typed variable before
// marshalling. Assigning a typed pointer into an `any` would wrap a nil pointer
// in a non-nil interface, so NeuralWatt's `nil` free-tier result must be
// detected on the typed value, not via an interface `== nil` check.
func fetchQuotaSnapshot(ctx context.Context, disc *provider.DiscoveryService, prov *provider.Provider, masterKey string) (string, json.RawMessage, int, error) {
	providerType := provider.DetectProviderType(prov.BaseURL)
	kind, ok := quotaKindFor(providerType)
	if !ok {
		return "", nil, 0, errors.New("provider type does not expose quota")
	}

	marshal := func(v any) (json.RawMessage, int, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, 0, err
		}
		return b, http.StatusOK, nil
	}

	var (
		payload json.RawMessage
		status  int
		err     error
	)
	switch providerType {
	case "nanogpt":
		var res *provider.NanoGPTUsageResponse
		if res, err = disc.GetNanoGPTUsage(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "zai-coding":
		var res *provider.ZAICodingQuotaResponse
		if res, err = disc.GetZAICodingQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "kimi-code":
		var res *provider.KimiCodeQuotaResponse
		if res, err = disc.GetKimiCodeQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "minimax":
		var res *provider.MiniMaxQuotaResponse
		if res, err = disc.GetMiniMaxQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "openrouter":
		var res *provider.OpenRouterBalance
		if res, err = disc.GetOpenRouterBalance(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "neuralwatt":
		var res *provider.NeuralWattQuotaResponse
		if res, err = disc.GetNeuralWattQuota(ctx, prov, masterKey); err == nil {
			if res == nil {
				return kind, json.RawMessage("null"), http.StatusNoContent, nil
			}
			payload, status, err = marshal(res)
		}
	case "deepseek":
		var res *provider.DeepSeekBalanceResponse
		if res, err = disc.GetDeepSeekBalance(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "ollama-cloud":
		var res *provider.OllamaCloudAccount
		if res, err = disc.GetOllamaCloudAccount(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	}
	if err != nil {
		if errors.Is(err, provider.ErrProviderKeyInvalid) {
			return kind, nil, http.StatusFailedDependency, nil
		}
		return kind, nil, 0, err
	}
	return kind, payload, status, nil
}

// PollQuotasOnce refreshes the snapshot for every enabled quota-capable
// provider. Called by the background quota loop. Each provider fetch is bounded
// by its own timeout so one slow upstream cannot stall the pass, and failures
// are recorded (via RecordFailure) without discarding the last good snapshot.
func (h *Handler) PollQuotasOnce(ctx context.Context) {
	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		debuglog.Error("quota: list providers failed", "error", err)
		return
	}
	disc := newDiscoveryService()
	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}
		if _, ok := quotaKindFor(provider.DetectProviderType(prov.BaseURL)); !ok {
			continue
		}
		provCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		kind, payload, status, ferr := fetchQuotaSnapshot(provCtx, disc, prov, h.cfg.MasterKey)
		if ferr != nil {
			debuglog.Warn("quota: poll fetch failed", "provider", prov.Name, "error", ferr)
			if rerr := h.quotaRepo.RecordFailure(provCtx, prov.ID, kind, ferr.Error()); rerr != nil {
				debuglog.Warn("quota: record failure failed", "provider", prov.Name, "error", rerr)
			}
		} else if uerr := h.quotaRepo.Upsert(provCtx, quota.Snapshot{
			ProviderID: prov.ID,
			Kind:       kind,
			Payload:    payload,
			HTTPStatus: status,
			Source:     "poll",
		}); uerr != nil {
			debuglog.Warn("quota: poll upsert failed", "provider", prov.Name, "error", uerr)
		}
		cancel()
	}
}
