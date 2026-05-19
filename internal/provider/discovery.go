package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// DiscoveryService handles model discovery across different LLM providers.
type DiscoveryService struct {
	httpClient *http.Client
	// quotaBreaker tracks per-provider circuit breaker state for quota fetches.
	// Key: providerID string, Value: *quotaCircuitState.
	quotaBreaker sync.Map
}

// NewDiscoveryService creates a new discovery service instance.
func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewDiscoveryServiceWithHTTPClient creates a discovery service with a custom
// HTTP client. This is intended for tests that need to inject a mock transport.
func NewDiscoveryServiceWithHTTPClient(client *http.Client) *DiscoveryService {
	return &DiscoveryService{httpClient: client}
}

// fetchURL makes an HTTP request with the given headers, reads the full
// response body, and checks for a 200 OK status. Returns the response body
// bytes on success. The caller is responsible for unmarshaling the result.
func (d *DiscoveryService) fetchURL(ctx context.Context, method, url string, headers http.Header) ([]byte, error) {
	//nolint:gocritic // url variable shadows import but context makes it clear
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, util.SanitizeLogBody(string(bodyBytes), 2000))
	}

	return bodyBytes, nil
}

// DetectProviderType parses the provider's base URL and returns a type string
// based on the hostname and (for some providers) the URL path. It uses exact
// host matching and suffix matching so that "https://my-proxy.deepseek.com"
// correctly resolves to "deepseek" rather than matching a substring like
// strings.Contains would.
func DetectProviderType(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		debuglog.Warn("discovery: failed to parse base URL", "url", baseURL)
		return "openai"
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)

	// Exact matches first
	switch host {
	case "api.nano-gpt.com", "nano-gpt.com":
		return "nanogpt"
	case "api.z.ai", "z.ai":
		return "zai-coding"
	case "api.deepseek.com", "deepseek.com":
		return "deepseek"
	case "api.anthropic.com", "anthropic.com":
		return "anthropic"
	case "api.x.ai", "x.ai":
		return "xai"
	case "api.cohere.com", "api.cohere.ai":
		return "cohere"
	case "openrouter.ai":
		return "openrouter"
	case "generativelanguage.googleapis.com":
		return "google"
	case "ollama.com":
		return "ollama-cloud"
	case "opencode.ai":
		// Path-based detection: Go URL contains /zen/go/, Zen contains /zen/
		// Must check Go before Zen since /zen/go/ is a subpath of /zen/
		if strings.Contains(path, "/zen/go") {
			return "opencode-go"
		}
		if strings.Contains(path, "/zen") {
			return "opencode-zen"
		}
	}

	// Subdomain matching: api.foo.deepseek.com, custom.nano-gpt.com, etc.
	if strings.HasSuffix(host, ".nano-gpt.com") {
		return "nanogpt"
	}
	if strings.HasSuffix(host, ".z.ai") {
		return "zai-coding"
	}
	if strings.HasSuffix(host, ".deepseek.com") {
		return "deepseek"
	}
	if strings.HasSuffix(host, ".anthropic.com") {
		return "anthropic"
	}
	if strings.HasSuffix(host, ".x.ai") {
		return "xai"
	}
	if strings.HasSuffix(host, ".cohere.com") || strings.HasSuffix(host, ".cohere.ai") {
		return "cohere"
	}
	if strings.HasSuffix(host, ".openrouter.ai") {
		return "openrouter"
	}
	if strings.HasSuffix(host, ".googleapis.com") {
		if strings.Contains(host, "generativelanguage") || strings.Contains(host, "aiplatform") {
			return "google"
		}
	}
	if strings.HasSuffix(host, ".ollama.com") {
		return "ollama-cloud"
	}
	if strings.HasSuffix(host, ".opencode.ai") {
		// Path-based detection for custom opencode.ai subdomains
		if strings.Contains(path, "/zen/go") {
			return "opencode-go"
		}
		if strings.Contains(path, "/zen") {
			return "opencode-zen"
		}
	}

	// Local providers (localhost with any port)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		// Port-based heuristics for common local providers
		port := u.Port()
		switch port {
		case "11434":
			return "ollama"
		case "5001":
			return "koboldcpp"
		case "1234":
			return "lmstudio"
		}

		return "openai"
	}

	return "openai"
}

// DiscoverModels discovers available models from a provider.
func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	providerType := DetectProviderType(provider.BaseURL)
	debuglog.Info("discovery: starting discovery", "provider", provider.Name, "provider_id", provider.ID, "type", providerType)

	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(provider.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		apiKey, err = auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
		if err != nil {
			debuglog.Error("discovery: failed to decrypt API key", "provider", provider.Name, "provider_id", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to decrypt API key: %w", err)
		}
	}

	models, err := func() ([]*model.Model, error) {
		switch providerType {
		case "nanogpt":
			return d.discoverNanoGPT(ctx, provider, apiKey)
		case "zai-coding":
			return d.discoverZAICoding(ctx, provider, apiKey)
		case "deepseek":
			return d.discoverDeepSeek(ctx, provider, apiKey)
		case "anthropic":
			return d.discoverAnthropic(ctx, provider, apiKey)
		case "ollama":
			return d.discoverOllama(ctx, provider, apiKey)
		case "ollama-cloud":
			// Ollama Cloud (ollama.com) reuses the same /api/tags + /api/show
			// discovery endpoints as local Ollama. If the cloud API diverges
			// in the future, this will need a dedicated discoverer.
			return d.discoverOllama(ctx, provider, apiKey)
		case "opencode-zen":
			return d.discoverOpenCodeZen(ctx, provider, apiKey)
		case "opencode-go":
			return d.discoverOpenCodeGo(ctx, provider, apiKey)
		case "xai":
			return d.discoverXAI(ctx, provider, apiKey)
		case "google":
			return d.discoverGoogleAIStudio(ctx, provider, apiKey)
		case "cohere":
			return d.discoverCohere(ctx, provider, apiKey)
		case "openrouter":
			return d.discoverOpenRouter(ctx, provider, apiKey)
		case "koboldcpp":
			return d.discoverKoboldCPP(ctx, provider, apiKey)
		case "lmstudio":
			return d.discoverLMStudio(ctx, provider, apiKey)
		default:
			return d.discoverOpenAI(ctx, provider, apiKey)
		}
	}()
	if err != nil {
		debuglog.Error("discovery: discovery failed", "provider", provider.Name, "provider_id", provider.ID, "type", providerType, "error", err)
		return nil, err
	}

	debuglog.Info("discovery: completed", "provider", provider.Name, "provider_id", provider.ID, "models", len(models))
	return models, nil
}

const maxQuotaRetries = 3

// Circuit breaker thresholds.
const (
	// quotaBreakerThreshold is the number of consecutive failures before the
	// circuit opens and further quota fetches are short-circuited.
	quotaBreakerThreshold = 5
	// quotaBreakerResetAfter is how long an open circuit stays open before
	// transitioning to half-open, allowing one probe request through.
	quotaBreakerResetAfter = 5 * time.Minute
)

// quotaCircuitState tracks consecutive failures for a single provider.
type quotaCircuitState struct {
	mu             sync.Mutex
	consecFailures int
	openUntil      time.Time // zero means closed; set when circuit opens
}

// isCircuitOpen returns true if the circuit is open (requests should be
// short-circuited). If the open window has expired, it transitions to
// half-open and returns false (allowing one probe).
func (s *quotaCircuitState) isCircuitOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openUntil.IsZero() {
		return false
	}
	if time.Now().Before(s.openUntil) {
		return true
	}
	// Half-open: allow one probe. Don't reset consecFailures yet; the
	// probe success will do that.
	s.openUntil = time.Time{}
	return false
}

// recordSuccess resets the circuit breaker state on a successful fetch.
func (s *quotaCircuitState) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecFailures = 0
	s.openUntil = time.Time{}
}

// recordFailure increments the failure counter and opens the circuit if the
// threshold is reached. Returns true if the circuit just opened.
func (s *quotaCircuitState) recordFailure() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecFailures++
	if s.consecFailures >= quotaBreakerThreshold && s.openUntil.IsZero() {
		s.openUntil = time.Now().Add(quotaBreakerResetAfter)
		return true
	}
	return false
}

// getOrCreateCircuit returns the circuit breaker state for a provider,
// creating one if it doesn't exist yet.
func (d *DiscoveryService) getOrCreateCircuit(providerID string) *quotaCircuitState {
	val, _ := d.quotaBreaker.LoadOrStore(providerID, &quotaCircuitState{})
	return val.(*quotaCircuitState)
}

// isTransientNetworkError returns true for DNS failures, timeouts, and
// connection errors that are likely to succeed on retry.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	// url.Error wraps underlying network errors
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTransientNetworkError(urlErr.Err)
	}
	return false
}

// isRetryableStatus returns true for HTTP status codes that warrant a retry.
func isRetryableStatus(statusCode int) bool {
	switch {
	case statusCode == http.StatusTooManyRequests: // 429
		return true
	case statusCode >= 500 && statusCode < 600: // 5xx
		return true
	default:
		return false
	}
}

// retryBackoff computes a linear backoff with jitter: base × attempt + random
// jitter in [0, base). This prevents thundering herd when multiple providers
// fail simultaneously.
func retryBackoff(base time.Duration, attempt int) time.Duration {
	delay := time.Duration(attempt) * base
	jitter := time.Duration(rand.Int64N(int64(base)))
	return delay + jitter
}

// doQuotaRequestWithRetry executes an HTTP request with retries for transient
// network errors (DNS failures, timeouts, connection issues) and retryable
// HTTP statuses (429, 5xx). The circuit breaker short-circuits requests to
// providers that have failed consecutively beyond the threshold.
// On success, the circuit breaker is reset automatically.
// On final failure, the circuit breaker failure counter is incremented.
func (d *DiscoveryService) doQuotaRequestWithRetry(ctx context.Context, req *http.Request, providerID, providerName, providerType string) (*http.Response, error) {
	circuit := d.getOrCreateCircuit(providerID)
	if circuit.isCircuitOpen() {
		debuglog.Warn("discovery: circuit breaker open, skipping quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID)
		return nil, fmt.Errorf("quota fetch circuit breaker open for provider %s (consecutive failures threshold reached)", providerName)
	}

	var lastErr error
	for attempt := range maxQuotaRetries {
		if attempt > 0 {
			backoff := retryBackoff(3*time.Second, attempt)
			debuglog.Info("discovery: retrying quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "backoff", backoff, "attempt", attempt+1, "max_attempts", maxQuotaRetries)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry: %w", lastErr)
			case <-time.After(backoff):
			}
		}
		//nolint:gosec // provider URL is admin-configured, not arbitrary user input
		resp, err := d.httpClient.Do(req)
		if err != nil {
			if isTransientNetworkError(err) {
				lastErr = err
				continue
			}
			circuit.recordFailure()
			return nil, err
		}
		// Retry on 429 (rate-limited) and 5xx (server error) responses.
		if isRetryableStatus(resp.StatusCode) {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("retryable HTTP %d: %s", resp.StatusCode, util.SanitizeLogBody(string(body), 200))
			debuglog.Info("discovery: retryable HTTP status for quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}
		// Success or non-retryable status — return as-is.
		circuit.recordSuccess()
		return resp, nil
	}
	if opened := circuit.recordFailure(); opened {
		debuglog.Warn("discovery: circuit breaker opened for quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "threshold", quotaBreakerThreshold)
	}
	return nil, fmt.Errorf("quota fetch failed for provider %s (type=%s) after %d attempts: %w", providerName, providerType, maxQuotaRetries, lastErr)
}
