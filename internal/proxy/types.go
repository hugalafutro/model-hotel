package proxy

import (
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
)

type contextKey string

const virtualKeyNameKey contextKey = "virtual_key_name"
const virtualKeyIDKey contextKey = "virtual_key_id"
const virtualKeyHashKey contextKey = "virtual_key_hash"

type requestLogData struct {
	id                    string
	providerID            uuid.UUID
	modelID               string
	requestHash           string
	providerName          string
	statusCode            int
	durationMs            float64
	proxyOverheadMs       float64
	parseMs               float64
	modelLookupMs         float64
	providerLookupMs      float64
	keyDecryptMs          float64
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
}

type modelCandidate struct {
	model    *model.Model
	provider *provider.Provider
	apiKey   string
}

type ChatCompletionRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        Message `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}
