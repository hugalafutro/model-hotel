package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AdminClient talks to the proxy's admin API to set up test fixtures.
type AdminClient struct {
	baseURL    string
	adminToken string
	client     *http.Client
}

// NewAdminClient creates an admin API client.
func NewAdminClient(proxyURL, adminToken string) *AdminClient {
	return &AdminClient{
		baseURL:    proxyURL,
		adminToken: adminToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateProviderResponse is the JSON response from creating a provider.
type CreateProviderResponse struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	BaseURL          string  `json:"base_url"`
	MaskedKey        string  `json:"masked_key"`
	Enabled          bool    `json:"enabled"`
	ModelCount       int     `json:"model_count"`
	TotalTokens      int     `json:"total_tokens"`
	LastDiscoveredAt *string `json:"last_discovered_at"`
	LastUsedAt       *string `json:"last_used_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// CreateProvider creates a provider pointing to the mock upstream.
func (a *AdminClient) CreateProvider(name, baseURL, apiKey string) (*CreateProviderResponse, error) {
	body := map[string]string{
		"name":     name,
		"base_url": baseURL,
		"api_key":  apiKey,
	}
	b, _ := json.Marshal(body)

	resp, err := a.do("POST", "/api/providers", b)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create provider returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateProviderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}
	return &result, nil
}

// DeleteProvider removes a provider by ID.
func (a *AdminClient) DeleteProvider(id string) error {
	resp, err := a.do("DELETE", "/api/providers/"+id, nil)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete provider returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// CreateVirtualKeyResponse is the response from creating a virtual key.
type CreateVirtualKeyResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Key        string  `json:"key,omitempty"`
	KeyPreview string  `json:"key_preview"`
	TokensUsed int64   `json:"tokens_used"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// CreateVirtualKey creates a new virtual key and returns the raw key value.
func (a *AdminClient) CreateVirtualKey(name string, rateLimitRPS *float64, rateLimitBurst *int) (*CreateVirtualKeyResponse, error) {
	body := map[string]interface{}{"name": name}
	if rateLimitRPS != nil {
		body["rate_limit_rps"] = *rateLimitRPS
	}
	if rateLimitBurst != nil {
		body["rate_limit_burst"] = *rateLimitBurst
	}
	b, _ := json.Marshal(body)

	resp, err := a.do("POST", "/api/virtual-keys", b)
	if err != nil {
		return nil, fmt.Errorf("create virtual key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create virtual key returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateVirtualKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode virtual key response: %w", err)
	}
	return &result, nil
}

// DeleteVirtualKey removes a virtual key by ID.
func (a *AdminClient) DeleteVirtualKey(id string) error {
	resp, err := a.do("DELETE", "/api/virtual-keys/"+id, nil)
	if err != nil {
		return fmt.Errorf("delete virtual key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete virtual key returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// UpdateVirtualKeyRateLimits updates per-key rate limits on a virtual key.
func (a *AdminClient) UpdateVirtualKeyRateLimits(id, name string, rps *float64, burst *int) error {
	body := map[string]interface{}{
		"name":             name,
		"rate_limit_rps":   rps,
		"rate_limit_burst": burst,
	}
	b, _ := json.Marshal(body)

	resp, err := a.do("PUT", "/api/virtual-keys/"+id, b)
	if err != nil {
		return fmt.Errorf("update virtual key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update virtual key returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// UpdateSettings updates proxy settings via the admin API.
func (a *AdminClient) UpdateSettings(settings map[string]string) error {
	b, _ := json.Marshal(settings)

	resp, err := a.do("PUT", "/api/settings", b)
	if err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update settings returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetSettings retrieves all current proxy settings.
func (a *AdminClient) GetSettings() (map[string]string, error) {
	resp, err := a.do("GET", "/api/settings", nil)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}
	return result, nil
}

// TriggerDiscovery triggers model discovery for a specific provider.
func (a *AdminClient) TriggerDiscovery(providerID string) error {
	resp, err := a.do("POST", "/api/providers/"+providerID+"/discover", nil)
	if err != nil {
		return fmt.Errorf("trigger discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trigger discovery returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (a *AdminClient) do(method, path string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, a.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.adminToken)
	req.Header.Set("Content-Type", "application/json")

	return a.client.Do(req)
}
