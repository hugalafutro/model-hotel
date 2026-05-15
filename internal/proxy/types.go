package proxy

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// VirtualKeyRepository defines the interface for virtual key operations.
// Used to enable mocking in tests while allowing the real *virtualkey.Repository
// to be used in production.
type VirtualKeyRepository interface {
	AddTokens(ctx context.Context, keyHash string, tokens int) error
	TouchLastUsed(ctx context.Context, keyHash string) error
	FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error)
	Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error)
	Delete(ctx context.Context, id string) error
}

// VirtualKeyInfo holds the subset of virtual key data needed by the proxy.
type VirtualKeyInfo struct {
	ID             string
	Name           string
	KeyHash        string
	KeyPreview     string
	TokensUsed     int64
	RateLimitRPS   *float64
	RateLimitBurst *int
}

type contextKey string

const virtualKeyNameKey contextKey = "virtual_key_name"
const virtualKeyIDKey contextKey = "virtual_key_id"

// VirtualKeyHashKey re-exports the shared context key from ctxkeys so
// existing code in this package can reference it without a package prefix.
// The canonical definition lives in internal/ctxkeys to avoid import cycles.
const VirtualKeyHashKey = ctxkeys.VirtualKeyHashKey

type requestLogData struct {
	id                    string
	providerID            uuid.UUID
	modelID               string
	requestHash           string
	statusCode            int
	durationMs            float64
	latencyMs             float64
	proxyOverheadMs       float64
	parseMs               float64
	modelLookupMs         float64
	providerLookupMs      float64
	keyDecryptMs          float64
	safeDialMs            float64
	settingsReadMs        float64
	ttftMs                float64
	tokensPerSecond       float64
	tokensPrompt          int
	tokensCompletion      int
	tokensPromptCacheHit  int
	tokensPromptCacheMiss int
	streaming             bool
	virtualKeyName        string
	virtualKeyID          string
	errorMessage          string
	failoverAttempt       int
	state                 string
	insertWg              sync.WaitGroup // signals when the async INSERT has completed
}

type modelCandidate struct {
	model    *model.Model
	provider *provider.Provider
	apiKey   string
}

// ChatCompletionRequest is the request body for /v1/chat/completions.
type ChatCompletionRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream,omitempty"`
}

// ChatCompletionResponse is the OpenAI-compatible response format.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single completion choice in the response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        Message `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

// Message represents a chat message with role and content.
type Message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
}

// Usage contains token usage statistics for a request.
type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}
