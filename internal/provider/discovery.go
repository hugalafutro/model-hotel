package provider

import (
	"context"
	"fmt"
	"net/http"
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

func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	if strings.Contains(provider.BaseURL, "nano-gpt.com") {
		return d.discoverNanoGPT(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "z.ai") {
		return d.discoverZAI(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "deepseek.com") {
		return d.discoverDeepSeek(ctx, provider, apiKey)
	}

	if strings.Contains(provider.BaseURL, "ollama.com") {
		return d.discoverOllama(ctx, provider, apiKey)
	}

	return d.discoverOpenAI(ctx, provider, apiKey)
}
