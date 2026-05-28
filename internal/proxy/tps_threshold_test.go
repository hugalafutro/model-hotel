package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTPSThreshold_MinGenerationMinimum tests that when generationDuration < 1ms
// (the minimum threshold), TPS falls back to using totalDuration instead of
// generationDuration to avoid absurd TPS values.
func TestTPSThreshold_MinGenerationMinimum(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
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

	// totalDuration=2ms, responseHeaderMs=1.5ms -> generationDuration=0.5ms
	// minGeneration = max(1.0, 2*0.05) = max(1.0, 0.1) = 1.0
	// 0.5 < 1.0, so fallback to totalDuration: TPS = 5/2*1000 = 2500
	startTime := time.Now().Add(-2 * time.Millisecond)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{responseHeaderMs: 1.5, cancelOrigin: "failover_timeout"})

	// TPS should use fallback (totalDuration) since generationDuration < minGeneration
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS from fallback, got %f", logData.tokensPerSecond)
	}
}

// TestTPSThreshold_MinGenerationPercent tests that when generationDuration < 5%
// of totalDuration, TPS falls back to using totalDuration.
func TestTPSThreshold_MinGenerationPercent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hello world"}}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}
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

	// totalDuration=100ms, responseHeaderMs=96ms -> generationDuration=4ms
	// minGeneration = max(1.0, 100*0.05) = max(1.0, 5.0) = 5.0
	// 4 < 5.0, so fallback to totalDuration: TPS = 50/100*1000 = 500
	startTime := time.Now().Add(-100 * time.Millisecond)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{responseHeaderMs: 96.0, cancelOrigin: "failover_timeout"})

	// TPS should use fallback (totalDuration) since generationDuration < minGeneration
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS from fallback, got %f", logData.tokensPerSecond)
	}
}

// TestTPSThreshold_NormalCalculation tests TPS calculation when generationDuration
// exceeds the minGeneration threshold.
func TestTPSThreshold_NormalCalculation(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"text"}}],"usage":{"prompt_tokens":50,"completion_tokens":100,"total_tokens":150}}
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

	// totalDuration=100ms, responseHeaderMs=20ms -> generationDuration=80ms
	// minGeneration = max(1.0, 100*0.05) = max(1.0, 5.0) = 5.0
	// 80 >= 5.0, so use generationDuration: TPS = 100/80*1000 = 1250
	startTime := time.Now().Add(-100 * time.Millisecond)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{responseHeaderMs: 20.0, cancelOrigin: "failover_timeout"})

	// TPS should use generationDuration since it exceeds threshold
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS, got %f", logData.tokensPerSecond)
	}
}

// TestTPSThreshold_TypicalStreaming tests TPS with typical streaming values.
func TestTPSThreshold_TypicalStreaming(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"response text"}}],"usage":{"prompt_tokens":20,"completion_tokens":100,"total_tokens":120}}
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

	// totalDuration=1000ms, responseHeaderMs=250ms -> generationDuration=750ms
	// minGeneration = max(1.0, 1000*0.05) = max(1.0, 50.0) = 50.0
	// 750 >= 50.0, so use generationDuration: TPS = 100/750*1000 ~= 133.3
	startTime := time.Now().Add(-1000 * time.Millisecond)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{responseHeaderMs: 250.0, cancelOrigin: "failover_timeout"})

	// TPS should be calculated normally
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS, got %f", logData.tokensPerSecond)
	}
}

// TestTPSThreshold_NonStreaming_MinGenerationMinimum tests non-streaming path
// with generationDuration < 1ms minimum threshold.
func TestTPSThreshold_NonStreaming_MinGenerationMinimum(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: 1234,
			Model:   "test-model",
			Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "hello"}}},
			Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		})
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test"}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID: "test-model", streaming: false, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(10 * time.Millisecond)

	// totalDuration ~= 12ms, responseHeaderMs ~= 11.5ms -> generationDuration ~= 0.5ms
	// minGeneration = max(1.0, 12*0.05) = 1.0
	// 0.5 < 1.0, so fallback to totalDuration
	startTime := time.Now().Add(-2 * time.Millisecond)
	h.handleNonStreamingResponse(inner, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 1.5, "", 1)

	// TPS should use fallback (totalDuration)
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS from fallback, got %f", logData.tokensPerSecond)
	}
}
