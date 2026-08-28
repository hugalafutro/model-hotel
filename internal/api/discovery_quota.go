package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// respondQuotaError maps a quota/usage/balance fetch error to an HTTP response.
// A dead upstream credential (provider.ErrProviderKeyInvalid) is a provider
// configuration problem, not a server fault: respond 424 Failed Dependency - a
// 4xx, so the access log records it at WARN rather than ERROR - and skip the
// ERROR line respondError would emit, since the fetch layer already logged the
// rejection once at WARN. The sidebar badge hides on any non-2xx, so the dead
// provider simply shows no badge instead of spamming errors on every poll.
// Anything else stays a logged 500.
func respondQuotaError(w http.ResponseWriter, providerName, resource string, err error) {
	if errors.Is(err, provider.ErrProviderKeyInvalid) {
		http.Error(w, fmt.Sprintf("provider key invalid or inactive for %s", providerName), http.StatusFailedDependency)
		return
	}
	respondError(w, fmt.Sprintf("failed to fetch %s for provider %s", resource, providerName), err, http.StatusInternalServerError)
}

// serveQuota returns the stored quota snapshot for prov, reproducing its status.
// On a cold miss it performs a one-time live fetch, persists it, then serves it,
// so a brand-new provider's first view is not blank. The X-Quota-Fetched-At
// header (RFC3339) carries the snapshot age for the client display.
func (h *Handler) serveQuota(w http.ResponseWriter, r *http.Request, prov *provider.Provider, expectedKind string) {
	providerType := provider.TypeOf(prov)
	kind, ok := quotaKindFor(providerType)
	if !ok || kind != expectedKind {
		// Enforce the endpoint contract: /usage, /balance and /account each
		// serve only their own kind. Without this a provider whose type maps to
		// a different kind would return the wrong payload shape on the endpoint.
		http.Error(w, expectedKind+" information not supported for this provider type", http.StatusBadRequest)
		return
	}

	snap, err := h.quotaRepo.Get(r.Context(), prov.ID, kind)
	if err != nil {
		respondError(w, "failed to load quota snapshot", err, http.StatusInternalServerError)
		return
	}
	if snap == nil {
		// Cold: fetch once and persist, then fall through to serve it. The
		// context is decoupled from the HTTP request deadline so a client
		// disconnect does not abort the in-flight upstream call mid-fetch.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		disc := h.discoveryService()
		k, payload, status, ferr := fetchQuotaSnapshot(ctx, disc, prov, h.cfg.MasterKey)
		if ferr != nil {
			respondQuotaError(w, prov.Name, kind, ferr)
			return
		}
		if uerr := h.quotaRepo.Upsert(ctx, quota.Snapshot{ProviderID: prov.ID, Kind: k, Payload: payload, HTTPStatus: status, Source: "manual"}); uerr != nil {
			respondError(w, "failed to persist quota snapshot", uerr, http.StatusInternalServerError)
			return
		}
		snap, _ = h.quotaRepo.Get(ctx, prov.ID, kind)
		if snap == nil {
			http.Error(w, "quota unavailable", http.StatusServiceUnavailable)
			return
		}
	}

	w.Header().Set("X-Quota-Fetched-At", snap.FetchedAt.UTC().Format(time.RFC3339))
	switch snap.HTTPStatus {
	case http.StatusNoContent:
		w.WriteHeader(http.StatusNoContent)
	case http.StatusFailedDependency:
		http.Error(w, "", http.StatusFailedDependency)
	default:
		w.Header().Set("Content-Type", "application/json")
		//nolint:gosec // G705 false positive: provider quota JSON body, not HTML; Content-Type is application/json
		_, _ = w.Write(snap.Payload)
	}
}

// GetProviderUsage serves usage/quota information for a provider from the
// read-through snapshot store (cold-filling on first view).
func (h *Handler) GetProviderUsage(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
		return
	}

	h.serveQuota(w, r, prov, "usage")
}

// GetProviderBalance serves balance information for a provider from the
// read-through snapshot store (cold-filling on first view).
func (h *Handler) GetProviderBalance(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
		return
	}

	h.serveQuota(w, r, prov, "balance")
}

// GetOllamaCloudAccount serves Ollama Cloud account info from the read-through
// snapshot store (cold-filling on first view).
func (h *Handler) GetOllamaCloudAccount(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
		return
	}

	h.serveQuota(w, r, prov, "account")
}

// QuotaRefreshResult holds the result of refreshing quotas for a single provider.
type QuotaRefreshResult struct {
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Refreshed    bool   `json:"refreshed"`
	Error        string `json:"error,omitempty"`
}

// RefreshAllQuotas refreshes quota information for all providers that support it.
func (h *Handler) RefreshAllQuotas(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
		return
	}

	discovery := h.discoveryService()

	var results []QuotaRefreshResult
	refreshed := 0
	failed := 0
	skipped := 0

	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}

		provCtx, provCancel := context.WithTimeout(context.Background(), 30*time.Second)

		providerType := provider.TypeOf(prov)
		result := QuotaRefreshResult{
			ProviderName: prov.Name,
			ProviderType: providerType,
		}

		// fetchQuotaSnapshot returns kind=="" for provider types that expose no
		// quota endpoint; those are skipped (and not added to results), matching
		// the prior behaviour where the type switch had no matching case.
		kind, payload, status, ferr := fetchQuotaSnapshot(provCtx, discovery, prov, h.cfg.MasterKey)
		if kind == "" {
			provCancel()
			skipped++
			continue
		}
		if ferr != nil {
			result.Error = ferr.Error()
			failed++
			_ = h.quotaRepo.RecordFailure(provCtx, prov.ID, kind, ferr.Error())
		} else if uerr := h.quotaRepo.Upsert(provCtx, quota.Snapshot{ProviderID: prov.ID, Kind: kind, Payload: payload, HTTPStatus: status, Source: "manual"}); uerr != nil {
			// A failed persist must not report success: the stored snapshot is
			// still stale, so surface it as a failure rather than "refreshed".
			debuglog.Warn("quota: manual refresh upsert failed", "provider", prov.Name, "error", uerr)
			result.Error = uerr.Error()
			failed++
		} else {
			result.Refreshed = true
			refreshed++
		}

		provCancel()
		results = append(results, result)
	}

	writeJSON(w, map[string]any{
		"results":   results,
		"refreshed": refreshed,
		"failed":    failed,
		"skipped":   skipped,
	})
}
