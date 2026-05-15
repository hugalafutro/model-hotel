package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	s := NewServer(":0")
	if s.ChunkCount != 15 {
		t.Errorf("default ChunkCount = %d, want 15", s.ChunkCount)
	}
	if s.ChunkDelay != 20*time.Millisecond {
		t.Errorf("default ChunkDelay = %v, want 20ms", s.ChunkDelay)
	}
	if s.TokensPerChunk != 3 {
		t.Errorf("default TokensPerChunk = %d, want 3", s.TokensPerChunk)
	}
	if s.InitialDelay != 10*time.Millisecond {
		t.Errorf("default InitialDelay = %v, want 10ms", s.InitialDelay)
	}
	if s.ErrorRate != 0 {
		t.Errorf("default ErrorRate = %f, want 0", s.ErrorRate)
	}
}

func TestHandleCompletions_NonStreaming(t *testing.T) {
	s := NewServer(":0")
	s.ChunkCount = 3
	s.TokensPerChunk = 2
	s.InitialDelay = 0
	s.ChunkDelay = 0

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	body := `{"model":"mock-model","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if result["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", result["object"])
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("expected non-empty choices array")
	}
}

func TestHandleCompletions_Streaming(t *testing.T) {
	s := NewServer(":0")
	s.ChunkCount = 3
	s.TokensPerChunk = 2
	s.InitialDelay = 0
	s.ChunkDelay = 0

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	body := `{"model":"mock-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	data := string(respBody)

	if !strings.Contains(data, "data: [DONE]") {
		t.Error("streaming response missing data: [DONE]")
	}
	if !strings.Contains(data, `"chat.completion.chunk"`) {
		t.Error("streaming response missing chat.completion.chunk object")
	}
}

func TestHandleCompletions_RejectParams(t *testing.T) {
	s := NewServer(":0")
	s.RejectParams = []string{"top_p"}

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	body := `{"model":"mock-model","stream":false,"messages":[{"role":"user","content":"hi"}],"top_p":0.9}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var errResp map[string]interface{}
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("invalid JSON error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object in response")
	}
	if !strings.Contains(errObj["message"].(string), "top_p") {
		t.Errorf("error message = %v, want mention of top_p", errObj["message"])
	}
}

func TestHandleCompletions_RejectParams_NoRejectedParam(t *testing.T) {
	s := NewServer(":0")
	s.RejectParams = []string{"top_p"}

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	// Request without top_p should succeed
	body := `{"model":"mock-model","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestModelsEndpoint(t *testing.T) {
	s := NewServer(":0")

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if result["object"] != "list" {
		t.Errorf("object = %v, want list", result["object"])
	}

	data, ok := result["data"].([]interface{})
	if !ok || len(data) == 0 {
		t.Error("expected non-empty data array")
	}

	model, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected model object")
	}
	if model["id"] != "mock-model" {
		t.Errorf("model id = %v, want mock-model", model["id"])
	}
}

func TestHealthEndpoint(t *testing.T) {
	s := NewServer(":0")

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStats(t *testing.T) {
	s := NewServer(":0")
	s.ChunkCount = 2
	s.TokensPerChunk = 1
	s.InitialDelay = 0
	s.ChunkDelay = 0

	ts := httptest.NewServer(s.newHandler())
	defer ts.Close()

	// Send a non-streaming request
	body := `{"model":"mock-model","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	resp, _ := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	resp.Body.Close()

	served, failed := s.Stats()
	if served != 1 {
		t.Errorf("served = %d, want 1", served)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
}

func TestURL(t *testing.T) {
	s := NewServer(":9090")
	expected := "http://localhost:9090/v1"
	if s.URL() != expected {
		t.Errorf("URL() = %q, want %q", s.URL(), expected)
	}
}

func TestChunkDelayForRequest_Fixed(t *testing.T) {
	s := NewServer(":0")
	s.ChunkDelay = 50 * time.Millisecond
	// Without StreamDurationMin, should always return fixed ChunkDelay
	for i := 0; i < 10; i++ {
		d := s.chunkDelayForRequest()
		if d != 50*time.Millisecond {
			t.Errorf("chunkDelayForRequest() = %v, want 50ms (fixed mode)", d)
		}
	}
}

func TestChunkDelayForRequest_RandomDuration(t *testing.T) {
	s := NewServer(":0")
	s.ChunkCount = 10
	s.StreamDurationMin = 3 * time.Second
	s.StreamDurationMax = 13 * time.Second

	// Each call should return a delay in [3s/10, 13s/10] = [300ms, 1300ms]
	minDelay := 3 * time.Second / 10
	maxDelay := 13 * time.Second / 10

	for i := 0; i < 100; i++ {
		d := s.chunkDelayForRequest()
		if d < minDelay || d > maxDelay {
			t.Errorf("chunkDelayForRequest() = %v, want range [%v, %v]", d, minDelay, maxDelay)
		}
	}
}

func TestChunkDelayForRequest_SameMinMax(t *testing.T) {
	s := NewServer(":0")
	s.ChunkCount = 5
	s.StreamDurationMin = 5 * time.Second
	s.StreamDurationMax = 5 * time.Second

	// When min == max, delay should always be exactly 5s/5 = 1s
	for i := 0; i < 10; i++ {
		d := s.chunkDelayForRequest()
		if d != 1*time.Second {
			t.Errorf("chunkDelayForRequest() = %v, want 1s (fixed duration mode)", d)
		}
	}
}
