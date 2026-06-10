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
		debuglog.Error("discovery: "+label+" "+resource+" non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return fmt.Errorf("%s: unexpected status code %d for provider %s", label, resp.StatusCode, provider.Name)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: failed to decode %s response for provider %s: %w", label, resource, provider.Name, err)
	}

	return nil
}
