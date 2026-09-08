package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
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
func quotaAuthError(label, apiKey string, p *Provider, status int, body []byte) error {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return nil
	}
	debuglog.Warn("discovery: "+label+" quota rejected: provider key invalid or inactive",
		"status", status, "provider", p.Name, "provider_id", p.ID,
		"body", util.MaskCredentialBounded(apiKey, string(body), 2000))
	return fmt.Errorf("%s: %w for provider %s (status %d)", label, ErrProviderKeyInvalid, p.Name, status)
}

// decryptProviderKey unwraps a provider's stored credential for a quota or
// usage fetch. label is the provider-family tag used in the error prefix. A
// provider with no stored key (a local server started without one) yields an
// empty key rather than a decrypt failure, so the fetch that follows fails on
// what the upstream says about the missing credential instead of on a crypto
// error about an empty salt.
func decryptProviderKey(p *Provider, masterKey, label string) (string, error) {
	if len(p.EncryptedKey) == 0 {
		return "", nil
	}
	apiKey, err := auth.Decrypt(p.EncryptedKey, p.KeyNonce, p.KeySalt, masterKey)
	if err != nil {
		return "", fmt.Errorf("%s: failed to decrypt API key for provider %s: %w", label, p.Name, err)
	}
	return apiKey, nil
}

// fetchQuotaJSON runs the shared decrypt → GET → retry → decode flow used by
// provider quota/balance endpoints. label is the provider-family tag used in
// error prefixes, debug logs, and the retry metric (e.g. "deepseek").
// resource is the human-readable resource name for error messages (e.g. "balance", "usage").
// expected lists statuses a caller handles itself, which come back as a plain
// *httpError with no ERROR log.
func (d *DiscoveryService) fetchQuotaJSON(ctx context.Context, provider *Provider, masterKey, path, label, resource string, out any, expected ...int) error {
	apiKey, err := decryptProviderKey(provider, masterKey, label)
	if err != nil {
		return err
	}
	return d.fetchQuotaJSONAt(ctx, provider, apiKey, "GET", util.SanitizeBaseURL(provider.BaseURL)+path, label, resource, out, expected...)
}

// fetchQuotaJSONAt is fetchQuotaJSON from the decrypted key onwards, for the
// fetchers whose URL is not baseURL+path (an absolute vendor URL, a base with
// the /v1 suffix stripped) or whose method is not GET. A non-200 status comes
// back as an *httpError, so a caller that treats one status specially reads it
// with errorStatusCode. A status the caller listed in expected is that caller's
// normal case, so it is neither logged at ERROR nor classified as an auth
// rejection: it comes back as a bare *httpError to branch on.
func (d *DiscoveryService) fetchQuotaJSONAt(ctx context.Context, provider *Provider, apiKey, method, fullURL, label, resource string, out any, expected ...int) error {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, http.NoBody)
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, httpx.MaxErrorBody))
		if slices.Contains(expected, resp.StatusCode) {
			// The body travels with the status: an expected status is the
			// caller's normal case only for the bodies it recognises, and it
			// cannot tell them apart from the code alone.
			return &httpError{StatusCode: resp.StatusCode, Body: body}
		}
		if authErr := quotaAuthError(label, apiKey, provider, resp.StatusCode, body); authErr != nil {
			return authErr
		}
		debuglog.Error("discovery: "+label+" "+resource+" non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.MaskCredentialBounded(apiKey, string(body), 2000))
		return &httpError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s: unexpected status code %d for provider %s", label, resp.StatusCode, provider.Name),
		}
	}

	if err := httpx.DecodeCappedJSON(resp.Body, httpx.MaxUpstreamBody, out); err != nil {
		return fmt.Errorf("%s: failed to decode %s response for provider %s: %w", label, resource, provider.Name, err)
	}

	return nil
}
