package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/model"
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
		return "openai"
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)

	// Exact matches first
	switch host {
	case "api.nano-gpt.com", "nano-gpt.com":
		return "nanogpt"
	case "api.z.ai", "z.ai":
		return "zai"
	case "api.deepseek.com", "deepseek.com":
		return "deepseek"
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
		return "zai"
	}
	if strings.HasSuffix(host, ".deepseek.com") {
		return "deepseek"
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
	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(provider.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		apiKey, err = auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt API key: %w", err)
		}
	}

	switch DetectProviderType(provider.BaseURL) {
	case "nanogpt":
		return d.discoverNanoGPT(ctx, provider, apiKey)
	case "zai":
		return d.discoverZAI(ctx, provider, apiKey)
	case "deepseek":
		return d.discoverDeepSeek(ctx, provider, apiKey)
	case "ollama":
		return d.discoverOllama(ctx, provider, apiKey)
	case "opencode-zen":
		return d.discoverOpenCodeZen(ctx, provider, apiKey)
	case "opencode-go":
		return d.discoverOpenCodeGo(ctx, provider, apiKey)
	default:
		return d.discoverOpenAI(ctx, provider, apiKey)
	}
}
