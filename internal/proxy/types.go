package proxy

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ModelRepository defines the interface for model operations.
// Used to enable mocking in tests while allowing the real *model.Repository
// to be used in production.
type ModelRepository interface {
	ListEnabled(ctx context.Context) ([]*model.Model, error)
	Upsert(ctx context.Context, model *model.Model) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (*model.Model, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
	GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error)
}

// VirtualKeyRepository defines the interface for virtual key operations.
// Used to enable mocking in tests while allowing the real *virtualkey.Repository
// to be used in production.
type VirtualKeyRepository interface {
	AddTokens(ctx context.Context, keyHash string, tokens int) error
	TouchLastUsed(ctx context.Context, keyHash string) error
	FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error)
	Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error)
	Delete(ctx context.Context, id string) error
}

// VirtualKeyInfo holds the subset of virtual key data needed by the proxy.
type VirtualKeyInfo struct {
	ID               string
	Name             string
	KeyHash          string
	KeyPreview       string
	TokensUsed       int64
	RateLimitRPS     *float64
	RateLimitBurst   *int
	RateLimitTPM     *int
	AllowedProviders *[]string
	StripReasoning   bool
	// Owner is set when the key belongs to a dashboard user; nil for unowned
	// keys. Populated only on the FindByKeyHash auth path.
	Owner *OwnerInfo
}

// OwnerInfo is the owning user's proxy-relevant state: identity for the
// shared "user:<uuid>" limiter bucket, the account enabled switch, and the
// aggregate per-user limits.
type OwnerInfo struct {
	ID             string
	Enabled        bool
	RateLimitRPS   *float64
	RateLimitBurst *int
	RateLimitTPM   *int
}

type contextKey string

const virtualKeyNameKey contextKey = "virtual_key_name"
const virtualKeyIDKey contextKey = "virtual_key_id"

// VirtualKeyHashKey re-exports the shared context key from ctxkeys so
// existing code in this package can reference it without a package prefix.
// The canonical definition lives in internal/ctxkeys to avoid import cycles.
const VirtualKeyHashKey = ctxkeys.VirtualKeyHashKey

// Endpoint families recorded in request_logs.endpoint_type. The proxy serves
// chat completions plus the multimodal OpenAI-compatible endpoints; every
// request log row is tagged with the family it came through.
const (
	endpointTypeChat       = "chat"
	endpointTypeMessages   = "messages"
	endpointTypeEmbeddings = "embeddings"
	endpointTypeRerank     = "rerank"
	endpointTypeImage      = "image"
	endpointTypeTTS        = "tts"
	endpointTypeSTT        = "stt"
)

type requestLogData struct {
	id                        string
	providerID                uuid.UUID
	providerName              string
	modelID                   string
	requestHash               string
	statusCode                int
	durationMs                float64
	latencyMs                 float64
	proxyOverheadMs           float64
	parseMs                   float64
	failoverLookupMs          float64
	modelLookupMs             float64
	providerLookupMs          float64
	keyDecryptMs              float64
	dialMs                    float64
	settingsReadMs            float64
	cacheHits                 resolveCacheHits
	responseHeaderMs          float64
	ttftMs                    float64
	tokensPerSecond           float64
	tokensPrompt              int
	tokensCompletion          int
	tokensCompletionReasoning int
	tokensPromptCacheHit      int
	tokensPromptCacheMiss     int
	streaming                 bool
	virtualKeyName            string
	virtualKeyID              string
	errorMessage              string
	errorKind                 ErrorKind // machine-readable classification; "" = unclassified (NULL in DB)
	failoverAttempt           int
	state                     string
	resolvedModelID           string
	endpointType              string         // endpoint family: chat, embeddings, rerank, image, tts, stt
	insertWg                  sync.WaitGroup // signals when the async INSERT has completed
}

type modelCandidate struct {
	model    *model.Model
	provider *provider.Provider
	apiKey   string
}

// requestState is the per-request scratch threaded through the ChatCompletions
// phases (ingest → resolve → config → failover loop), replacing the ~20 closure
// locals the handler previously carried. It is built by ingestRequest and
// augmented by later phases. Helpers mutate the shared pointer instance — never
// a copy — so timing/overhead accumulation is visible to subsequent phases.
type requestState struct {
	startTime   time.Time
	reqModel    string
	isStreaming bool
	vkHash      string
	bodyBytes   []byte
	parseMs     float64
	logData     *requestLogData

	// Multimodal pass-through fields (zero values = chat behavior).
	// endpointPath is the upstream path suffix ("" = "/chat/completions").
	// makeUpstreamBody, when set, replaces the chat-specific body rewrite in
	// buildCandidateRequest: it receives the resolved upstream model ID and
	// returns the upstream body plus its Content-Type.
	// longRunning marks endpoints whose legitimate latency rivals streaming
	// chat (image generation, audio synthesis/transcription); it grants the
	// same extended per-attempt timeout budget as isStreaming without
	// implying chat-stream semantics (body rewrite, breaker probe deferral).
	endpointPath     string
	makeUpstreamBody func(resolvedModelID string) (body []byte, contentType string, err error)
	longRunning      bool

	// Native Anthropic /v1/messages passthrough (zero values = translated path).
	// anthropicIn marks a request that arrived on /v1/messages (so an
	// Anthropic-family candidate is forwarded its original Messages body
	// natively instead of the OpenAI translation). anthropicRawBody is that
	// original body. anthropicNativeAttempt is set per failover attempt by
	// buildCandidateRequest: true when the current candidate is being served the
	// native path, read by the response dispatch and the response writer so they
	// forward Anthropic bytes verbatim instead of translating.
	anthropicIn            bool
	anthropicRawBody       []byte
	anthropicNativeAttempt bool

	// Populated by resolveCandidates (phase B).
	timings    resolveTimings
	cacheHits  resolveCacheHits
	isFailover bool

	// Populated by loadFailoverConfig (phase C).
	proxyOverhead         float64
	failoverTimeout       time.Duration
	overallDeadline       time.Time
	circuitBreakerEnabled bool
	// Request hedging (streaming only): when hedgingEnabled, a streaming
	// failover group races a backup provider hedgeDelay after the first stays
	// silent, keeping whichever returns its first token first.
	hedgingEnabled bool
	hedgeDelay     time.Duration

	// Accumulated across failover attempts (phase D / E). lastReqErr is the
	// structured cause of the most recent attempt's failure; lastErr is its
	// rendered string, kept in sync by setReqErr for the debug-log "error"
	// field. The exhaustion path renders the terminal message/status from
	// lastReqErr.
	lastErr    string
	lastReqErr reqError
}

// setReqErr records the structured cause of the most recent failed attempt and
// keeps the rendered lastErr string in sync. Every failover-loop site that used
// to assign st.lastErr a fmt.Sprintf string now goes through here so the
// exhaustion path always has a structured error to render and classify.
func (st *requestState) setReqErr(e reqError) {
	st.lastReqErr = e
	st.lastErr = e.render()
}

// candidateOutcome is the result of a single failover attempt
// (attemptCandidate): whether the caller should try the next candidate, has
// already served the client, or has written a terminal error.
type candidateOutcome int

const (
	// outcomeFailover: this candidate failed; try the next one (continue).
	outcomeFailover candidateOutcome = iota
	// outcomeServed: the response was fully handled; return.
	outcomeServed
	// outcomeFatal: a terminal error response was written; return.
	outcomeFatal
)

// streamOptions consolidates the parameters for handleStreamingResponse into
// a single struct, replacing 17 positional parameters with named fields.
type streamOptions struct {
	preReadBuf         *bytes.Buffer // nil = no TTFT probe (immediate commit)
	trueTtftMs         float64       // measured during TTFT probe, 0 if [DONE] first
	responseHeaderMs   float64       // time to HTTP headers from upstream
	streamStallTimeout time.Duration // 0 = no stall watchdog
	providerID         uuid.UUID
	providerName       string
	circuitBreakerOn   bool
	// timing fields
	proxyOverheadMs  float64
	parseMs          float64
	failoverLookupMs float64
	modelLookupMs    float64
	providerLookupMs float64
	keyDecryptMs     float64
	dialMs           float64
	settingsReadMs   float64
	vkHash           string
	attempt          int
	cancelOrigin     string
	// rawPassthrough forwards each data chunk verbatim instead of parsing it as
	// an OpenAI chunk and applying the transforms. Set for the native Anthropic
	// /v1/messages passthrough path, whose stream is already Anthropic-shaped.
	rawPassthrough bool
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
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
	// ToolCalls/ToolCallID must round-trip through the non-streaming decode +
	// re-encode in handleNonStreamingResponse, or function calls are silently
	// dropped (finish_reason:"tool_calls" with no tool_calls array) for every
	// non-streaming client. omitempty keeps plain text responses unchanged.
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	Reasoning        string            `json:"reasoning,omitempty"`         // Ollama, OpenRouter
	ReasoningDetails []ReasoningDetail `json:"reasoning_details,omitempty"` // OpenRouter, MiniMax
}

// ToolCall is an OpenAI function tool call on an assistant message. Preserved
// verbatim through the proxy so non-streaming clients receive the call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Index    *int         `json:"index,omitempty"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc is the function name + raw JSON arguments of a tool call.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// PromptTokensDetails breaks down prompt tokens into sub-categories.
// OpenAI returns cached token counts in this nested object rather than
// at the top level of usage. Third-party providers (Wafer AI, OpenRouter,
// NanoGPT) that normalise to OpenAI format also use this structure.
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// CompletionTokensDetails breaks down completion tokens into sub-categories
// (e.g. reasoning vs text). Providers like Anthropic report reasoning tokens
// separately from visible text tokens in this nested object.
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// Usage contains token usage statistics for a request.
type Usage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	PromptCacheHitTokens     int                      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens    int                      `json:"prompt_cache_miss_tokens,omitempty"`
	CacheReadInputTokens     int                      `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int                      `json:"cache_creation_input_tokens,omitempty"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}
