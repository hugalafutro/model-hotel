package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOpenCodeGo(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	headers := bearerHeader(apiKey)
	headers.Set("Content-Type", "application/json")

	catalog := opencodeCatalogModels(GetOpenCodeGoCatalog(), provider.ID, "opencode")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		// If the /models endpoint is gone (404), fall back to the catalog. The
		// catalog is an override channel that is normally empty, so this usually
		// yields no models — which keeps RecordMissingModels a no-op rather than
		// disabling anything, at the cost of a discovery.suspect_scan warning per
		// scan while the 404 persists (an empty result trips the blackout guard in
		// ConfirmMissingModels). Every other failure returns an error so a
		// transient outage aborts the scan instead of disabling live-only models.
		if errorStatusCode(err) == http.StatusNotFound {
			debuglog.Warn("discovery: opencode-go /models returned 404, falling back to catalog", "provider", provider.Name, "provider_id", provider.ID)
			return catalog, nil
		}
		debuglog.Error("discovery: opencode-go http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to fetch models for provider %s: %w", provider.Name, statusOnly(err))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-go json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to decode response for provider %s: %w", provider.Name, err)
	}

	// Live listing only carries id + owner; merge unions it with the catalog
	// (live wins, catalog backfills metadata and adds models the listing omits).
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}
	// Empty-but-successful listing: return empty rather than the catalog so
	// RecordMissingModels stays a no-op instead of disabling live-only models.
	if len(live) == 0 {
		debuglog.Warn("discovery: opencode-go /models returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	merged := mergeLiveAndCatalog(live, catalog)
	debuglog.Info("discovery: opencode-go discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalog), "merged", len(merged))
	return merged, nil
}

// GetOpenCodeGoUsage retrieves the subscription usage windows for an OpenCode
// Go provider from its /usage endpoint.
//
// 403 EntitlementError means the key is fine but carries no active Go
// subscription, so 403 is passed as an expected status: fetchQuotaJSONAt returns
// an expected status as an *httpError carrying the body, before quotaAuthError
// can classify 401/403 as a dead credential. The body decides which 403 this
// is. An EntitlementError reports no data and no error, which makes
// marshalQuota store 204 with a null payload so the badge stays hidden; any
// other 403 is the ordinary auth rejection every other quota fetcher reports,
// and becomes ErrProviderKeyInvalid (424), as does the 401 a revoked key
// answers.
func (d *DiscoveryService) GetOpenCodeGoUsage(ctx context.Context, provider *Provider, masterKey string) (*OpenCodeGoUsageResponse, error) {
	var usage OpenCodeGoUsageResponse
	err := d.fetchQuotaJSON(ctx, provider, masterKey, "/usage", "opencode-go", "usage", &usage, http.StatusForbidden)
	if errorStatusCode(err) == http.StatusForbidden {
		if openCodeGoNoSubscription(err) {
			debuglog.Info("discovery: opencode-go usage endpoint refused: no active Go subscription", "provider", provider.Name, "provider_id", provider.ID)
			return nil, nil
		}
		// The body is deliberately not logged: it is the one thing here that
		// could carry the credential back, and unlike quotaAuthError this path
		// no longer holds the decrypted key to mask it with.
		debuglog.Warn("discovery: opencode-go usage rejected: provider key invalid or inactive",
			"status", http.StatusForbidden, "provider", provider.Name, "provider_id", provider.ID)
		return nil, fmt.Errorf("opencode-go: %w for provider %s (status %d)", ErrProviderKeyInvalid, provider.Name, http.StatusForbidden)
	}
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

// openCodeGoNoSubscription reports whether a 403 body is OpenCode Go saying the
// key carries no active Go subscription, rather than rejecting the key itself.
// The shape is {"type":"error","error":{"type":"EntitlementError"}}; anything
// else, an unparseable body included, is not that claim. The type name is
// matched case-insensitively: a casing drift upstream would otherwise turn
// every key without a subscription into an invalid-key badge.
func openCodeGoNoSubscription(err error) bool {
	httpErr := &httpError{}
	if !errors.As(err, &httpErr) {
		return false
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(httpErr.Body, &body) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(body.Error.Type), "EntitlementError")
}
