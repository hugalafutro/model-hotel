package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

type DiscoveryService struct {
	httpClient *http.Client
}

func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DetectProviderType parses the provider's base URL and returns a type string
// based on the hostname and (for some providers) the URL path. It uses exact
// host matching and suffix matching so that "https://my-proxy.deepseek.com"
// correctly resolves to "deepseek" rather than matching a substring like
// strings.Contains would.
func DetectProviderType(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		log.Printf("[discovery] warning: failed to parse base URL %q, falling back to openai", baseURL)
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
	case "generativelanguage.googleapis.com":
		return "google"
	case "ollama.com":
		return "ollama"
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
	if strings.HasSuffix(host, ".googleapis.com") {
		if strings.Contains(host, "generativelanguage") || strings.Contains(host, "aiplatform") {
			return "google"
		}
	}
	if strings.HasSuffix(host, ".ollama.com") {
		return "ollama"
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

	// Local Ollama instances (localhost with any port)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		if strings.Contains(host, "ollama") {
			return "ollama"
		}
		return "openai"
	}

	return "openai"
}

func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	providerType := DetectProviderType(provider.BaseURL)
	log.Printf("[discovery] starting discovery for provider %s (type=%s)", provider.ID, providerType)

	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(provider.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		apiKey, err = auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
		if err != nil {
			log.Printf("[discovery] error: failed to decrypt API key for provider %s: %v", provider.ID, err)
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
		case "opencode-zen":
			return d.discoverOpenCodeZen(ctx, provider, apiKey)
		case "opencode-go":
			return d.discoverOpenCodeGo(ctx, provider, apiKey)
		case "xai":
			return d.discoverXAI(ctx, provider, apiKey)
		case "google":
			return d.discoverGoogleAIStudio(ctx, provider, apiKey)
		default:
			return d.discoverOpenAI(ctx, provider, apiKey)
		}
	}()
	if err != nil {
		log.Printf("[discovery] error: discovery failed for provider %s (type=%s): %v", provider.ID, providerType, err)
		return nil, err
	}

	log.Printf("[discovery] completed for provider %s: %d models found", provider.ID, len(models))
	return models, nil
}

const maxQuotaRetries = 3

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

// doQuotaRequestWithRetry executes an HTTP request with retries for transient
// network errors (DNS failures, timeouts, connection issues). Non-transient
// errors and successful responses are returned immediately.
func (d *DiscoveryService) doQuotaRequestWithRetry(ctx context.Context, req *http.Request, providerID, providerType string) (*http.Response, error) {
	var lastErr error
	for attempt := range maxQuotaRetries {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 3 * time.Second
			log.Printf("[discovery] %s provider %s: retrying quota fetch in %v (attempt %d/%d)", providerType, providerID, backoff, attempt+1, maxQuotaRetries)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry: %w", lastErr)
			case <-time.After(backoff):
			}
		}
		resp, err := d.httpClient.Do(req)
		if err != nil {
			if isTransientNetworkError(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		return resp, nil
	}
	return nil, fmt.Errorf("quota fetch failed after %d attempts: %w", maxQuotaRetries, lastErr)
}
