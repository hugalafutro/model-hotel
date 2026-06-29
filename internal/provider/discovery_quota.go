package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ErrProviderKeyInvalid indicates an upstream rejected the provider's stored
// credential (HTTP 401/403): the key is missing, revoked, or inactive. Quota /
// usage / balance fetchers return it (wrapped) so API handlers can surface a
// dead key as a 4xx provider-config condition instead of a 500, and log it at
// WARN rather than spamming ERROR on every sidebar badge poll.
var ErrProviderKeyInvalid = errors.New("provider key invalid or inactive")

// quotaAuthError classifies a non-200 quota/usage/balance response. For an
// upstream auth rejection (401/403) it logs once at WARN and returns an
// ErrProviderKeyInvalid-wrapped error; for any other status it returns nil so
// the caller falls through to its existing ERROR-logged handling of a genuinely
// unexpected status. label is the provider-family tag (e.g. "neuralwatt").
func quotaAuthError(label string, p *Provider, status int, body []byte) error {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return nil
	}
	debuglog.Warn("discovery: "+label+" quota rejected: provider key invalid or inactive",
		"status", status, "provider", p.Name, "provider_id", p.ID,
		"body", util.SanitizeLogBody(string(body), 2000))
	return fmt.Errorf("%s: %w for provider %s (status %d)", label, ErrProviderKeyInvalid, p.Name, status)
}

// fetchQuotaJSON runs the shared decrypt → GET → retry → decode flow used by
// provider quota/balance endpoints. label is the provider-family tag used in
// error prefixes, debug logs, and the retry metric (e.g. "deepseek").
// resource is the human-readable resource name for error messages (e.g. "balance", "usage").
func (d *DiscoveryService) fetchQuotaJSON(ctx context.Context, provider *Provider, masterKey, path, label, resource string, out any) error {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return fmt.Errorf("%s: failed to decrypt API key for provider %s: %w", label, provider.Name, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return fmt.Errorf("%s: failed to create request for provider %s: %w", label, provider.Name, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), provider.Name, label)
	if err != nil {
		return fmt.Errorf("%s: failed to fetch %s for provider %s: %w", label, resource, provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if authErr := quotaAuthError(label, provider, resp.StatusCode, body); authErr != nil {
			return authErr
		}
		debuglog.Error("discovery: "+label+" "+resource+" non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return fmt.Errorf("%s: unexpected status code %d for provider %s", label, resp.StatusCode, provider.Name)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: failed to decode %s response for provider %s: %w", label, resource, provider.Name, err)
	}

	return nil
}
