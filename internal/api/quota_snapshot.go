package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

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
		// We have no fresh provider list to rebuild advice from, and the last
		// computed map could be arbitrarily stale by the time providers are
		// listable again. Fail closed: clear it rather than let a frozen
		// deadline keep pinning a circuit's cooldown.
		h.ClearQuotaAdvice(ctx)
		return
	}
	disc := newDiscoveryService()
	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}
		kind, ok := quotaKindFor(provider.DetectProviderType(prov.BaseURL))
		if !ok {
			continue
		}

		// Fleet dedup: if Front Desk recently distributed a snapshot for this
		// provider, skip the upstream call. The primary (and any node FD is not
		// feeding) has no recent fleet snapshot and still self-polls, so quota is
		// never worse than standalone.
		interval := time.Duration(h.settingsRepo.GetInt(ctx, "quota_refresh_interval_min", 5)) * time.Minute
		if interval > 0 {
			if snap, _ := h.quotaRepo.Get(ctx, prov.ID, kind); snap != nil && snap.Source == "fleet" && time.Since(snap.FetchedAt) < interval {
				continue
			}
		}

		provCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, payload, status, ferr := fetchQuotaSnapshot(provCtx, disc, prov, h.cfg.MasterKey)
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

	h.RefreshQuotaAdvice(ctx)
}

// RefreshQuotaAdvice rebuilds the in-memory quota advice from stored snapshots.
// It reads the table rather than the poll's own fetches so fleet-distributed
// snapshots (which PollQuotasOnce skips) are included.
//
// A snapshot older than three refresh intervals is ignored. If quota polling
// is disabled (quota_refresh_interval_min <= 0) the resolved maxAge is <= 0,
// and buildQuotaAdvice treats that as "advise nothing": we cannot trust the
// age of any stored snapshot without a live poll cadence to bound it, so
// never pin the breaker on data that could predate a plan change or a manual
// top-up.
func (h *Handler) RefreshQuotaAdvice(ctx context.Context) {
	if h.quotaAdvisor == nil {
		debuglog.Warn("quota: advice refresh skipped, no advisor wired")
		return
	}
	snaps, err := h.quotaRepo.List(ctx)
	if err != nil {
		debuglog.Warn("quota: advice refresh failed to list snapshots", "error", err)
		return
	}
	interval := time.Duration(h.settingsRepo.GetInt(ctx, "quota_refresh_interval_min", 5)) * time.Minute
	maxAge := 3 * interval

	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		debuglog.Warn("quota: advice refresh failed to list providers", "error", err)
		return
	}
	typeByID := make(map[uuid.UUID]string, len(providers))
	nameByID := make(map[uuid.UUID]string, len(providers))
	for _, p := range providers {
		typeByID[p.ID] = provider.DetectProviderType(p.BaseURL)
		nameByID[p.ID] = p.Name
	}

	advice := buildQuotaAdvice(snaps, typeByID, maxAge, time.Now())
	h.quotaAdvisor.Replace(advice)
	// Info once anything is actually advised: a quota pin can hold a circuit open
	// for a day, and Debug is off in normal production, so this is the only log
	// trail an operator has for why a provider went dark. The no-advice case
	// stays at Debug — a line on every poll pass would be noise. Counts only, no
	// payload values.
	if len(advice) > 0 {
		debuglog.Info("quota: advice refreshed", "advised_providers", len(advice))
	} else {
		debuglog.Debug("quota: advice refreshed", "advised_providers", len(advice))
	}

	// Alert-only, and deliberately last: the schema-drift watch reuses the
	// snapshot list and provider maps above rather than re-querying, and
	// nothing it does can influence the advice just published.
	h.checkQuotaDrift(ctx, snaps, typeByID, nameByID, maxAge)
}

// ClearQuotaAdvice drops all quota advice immediately. Used whenever the
// in-memory map cannot be trusted to reflect current reality: quota polling
// has gone from enabled to disabled (the background loop stops calling
// RefreshQuotaAdvice entirely, so the last computed map would otherwise be
// retained for the process lifetime), or a poll pass could not even list
// providers. Safe to call when no advisor was ever wired (no-op).
func (h *Handler) ClearQuotaAdvice(_ context.Context) {
	if h.quotaAdvisor == nil {
		return
	}
	h.quotaAdvisor.Replace(nil)
}

// buildQuotaAdvice is the pure filtering step of RefreshQuotaAdvice, split out
// so the staleness rule and the assessment filter are testable without a
// database.
func buildQuotaAdvice(
	snaps []quota.Snapshot,
	typeByID map[uuid.UUID]string,
	maxAge time.Duration,
	now time.Time,
) map[uuid.UUID]time.Time {
	advice := make(map[uuid.UUID]time.Time)
	if maxAge <= 0 {
		// Quota polling is disabled (or the resolved interval is otherwise
		// non-positive): there is no cadence to bound snapshot age against, so
		// advise nothing rather than risk pinning the breaker on data that
		// could predate a plan change or a manual top-up.
		return advice
	}
	for _, s := range snaps {
		if now.Sub(s.FetchedAt) > maxAge {
			continue
		}
		a := quota.Assess(typeByID[s.ProviderID], s)
		if a.OK && a.Exhausted {
			advice[s.ProviderID] = a.ResetsAt
		}
	}
	return advice
}
