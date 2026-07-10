package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHandleStreamingResponse_PromptCacheTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 80 {
		t.Errorf("expected prompt_cache_hit=80, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 20 {
		t.Errorf("expected prompt_cache_miss=20, got %d", logData.tokensPromptCacheMiss)
	}
}

// ---------------------------------------------------------------------------
// Anthropic-native cache field fallback tests
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_AnthropicCacheTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"cache_read_input_tokens":60,"cache_creation_input_tokens":10}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 60 {
		t.Errorf("expected prompt_cache_hit=60, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 40 {
		t.Errorf("expected prompt_cache_miss=40, got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_AnthropicCacheOpenAITakesPrecedence(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Both OpenAI and Anthropic cache fields present - OpenAI should win
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"cache_read_input_tokens":60}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 80 {
		t.Errorf("expected prompt_cache_hit=80 (OpenAI takes precedence), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 20 {
		t.Errorf("expected prompt_cache_miss=20, got %d", logData.tokensPromptCacheMiss)
	}
}

// Anthropic cache with cache_read > prompt_tokens (negative miss clamped to 0)

func TestHandleStreamingResponse_AnthropicCacheNegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// cache_read_input_tokens (120) > prompt_tokens (100) → miss clamped to 0
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"cache_read_input_tokens":120,"cache_creation_input_tokens":10}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 120 {
		t.Errorf("expected prompt_cache_hit=120, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped), got %d", logData.tokensPromptCacheMiss)
	}
}

// ---------------------------------------------------------------------------
// PromptTokensDetails.cached_tokens fallback tests (tier 3)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_PromptTokensDetailsCachedTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// OpenAI's official nested format: prompt_tokens_details.cached_tokens
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"prompt_tokens_details":{"cached_tokens":1984},"completion_tokens_details":{"reasoning_tokens":261}}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 1984 {
		t.Errorf("expected prompt_cache_hit=1984 (from prompt_tokens_details.cached_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 24 {
		t.Errorf("expected prompt_cache_miss=24 (2008-1984), got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_AllCacheFormatsPrecedence(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// All three cache formats present - tier 1 (PromptCacheHitTokens) should win
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"prompt_cache_hit_tokens":500,"cache_read_input_tokens":300,"prompt_tokens_details":{"cached_tokens":1984}}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	// Tier 1 (PromptCacheHitTokens) should take precedence
	if logData.tokensPromptCacheHit != 500 {
		t.Errorf("expected prompt_cache_hit=500 (tier 1 takes precedence), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 1508 {
		t.Errorf("expected prompt_cache_miss=1508 (2008-500), got %d", logData.tokensPromptCacheMiss)
	}
}

// ---------------------------------------------------------------------------
// Negative cache miss clamping tests (max(0, prompt_tokens - cache_hit))
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_PromptTokensDetailsNegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream returns prompt_tokens: 100 but prompt_tokens_details.cached_tokens: 150
	// (more cached than total prompt). Verify miss is clamped to 0.
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":150}}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 150 {
		t.Errorf("expected prompt_cache_hit=150 (from prompt_tokens_details.cached_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped by max(0, 100-150)), got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_Tier2OverridesTier3(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Both tier 2 (cache_read_input_tokens) and tier 3 (prompt_tokens_details.cached_tokens) present.
	// Tier 1 is NOT present. Verify tier 2 wins.
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"cache_read_input_tokens":300,"prompt_tokens_details":{"cached_tokens":1984}}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	// Tier 2 (cache_read_input_tokens) should take precedence over tier 3
	if logData.tokensPromptCacheHit != 300 {
		t.Errorf("expected prompt_cache_hit=300 (tier 2: cache_read_input_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 1708 {
		t.Errorf("expected prompt_cache_miss=1708 (2008-300), got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_Tier1NegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Tier 1 (prompt_cache_hit_tokens: 500) > prompt_tokens: 400.
	// Verify miss is clamped to 0 by max(0, ...).
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":400,"completion_tokens":5,"total_tokens":405,"prompt_cache_hit_tokens":500}}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPromptCacheHit != 500 {
		t.Errorf("expected prompt_cache_hit=500 (from prompt_cache_hit_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped by max(0, 400-500)), got %d", logData.tokensPromptCacheMiss)
	}
}
