package harness

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- AdminClient tests ---

func TestAdminClient_CreateProvider(t *testing.T) {
	wantName := "test-provider"
	wantBaseURL := "http://localhost:9090/v1"
	wantAPIKey := "sk-test-key"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/providers" {
			t.Errorf("expected /api/providers, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-token" {
			t.Errorf("expected Authorization Bearer header, got %s", r.Header.Get("Authorization"))
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if body["name"] != wantName || body["base_url"] != wantBaseURL || body["api_key"] != wantAPIKey {
			t.Errorf("unexpected body: %v", body)
		}

		w.WriteHeader(http.StatusCreated)
		resp := CreateProviderResponse{
			ID:               "11111111-1111-1111-1111-111111111111",
			Name:             wantName,
			BaseURL:          wantBaseURL,
			MaskedKey:        "sk-tes...key",
			Enabled:          true,
			ModelCount:       0,
			TotalTokens:      0,
			LastDiscoveredAt: nil,
			LastUsedAt:       nil,
			CreatedAt:        "2024-01-01T00:00:00Z",
			UpdatedAt:        "2024-01-01T00:00:00Z",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	resp, err := admin.CreateProvider(wantName, wantBaseURL, wantAPIKey)
	if err != nil {
		t.Fatalf("CreateProvider() error: %v", err)
	}
	if resp.Name != wantName {
		t.Errorf("Name = %q, want %q", resp.Name, wantName)
	}
	if resp.BaseURL != wantBaseURL {
		t.Errorf("BaseURL = %q, want %q", resp.BaseURL, wantBaseURL)
	}
	if !resp.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if resp.MaskedKey != "sk-tes...key" {
		t.Errorf("MaskedKey = %q, want %q", resp.MaskedKey, "sk-tes...key")
	}
}

func TestAdminClient_CreateProvider_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	_, err := admin.CreateProvider("test", "http://localhost", "key")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestAdminClient_DeleteProvider(t *testing.T) {
	providerID := "22222222-2222-2222-2222-222222222222"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		expectedPath := "/api/providers/" + providerID
		if r.URL.Path != expectedPath {
			t.Errorf("expected %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	if err := admin.DeleteProvider(providerID); err != nil {
		t.Fatalf("DeleteProvider() error: %v", err)
	}
}

func TestAdminClient_DeleteProvider_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	err := admin.DeleteProvider("nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestAdminClient_CreateVirtualKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/virtual-keys" {
			t.Errorf("expected /api/virtual-keys, got %s", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if body["name"] != "test-key" {
			t.Errorf("expected name=test-key, got %s", body["name"])
		}

		w.WriteHeader(http.StatusCreated)
		resp := CreateVirtualKeyResponse{
			ID:         "33333333-3333-3333-3333-333333333333",
			Name:       "test-key",
			Key:        "vk-raw-key-value",
			KeyPreview: "vk-ra...lue",
			TokensUsed: 0,
			CreatedAt:  "2024-01-01T00:00:00Z",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	resp, err := admin.CreateVirtualKey("test-key")
	if err != nil {
		t.Fatalf("CreateVirtualKey() error: %v", err)
	}
	if resp.Name != "test-key" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-key")
	}
	if resp.Key != "vk-raw-key-value" {
		t.Errorf("Key = %q, want raw key", resp.Key)
	}
	if resp.TokensUsed != 0 {
		t.Errorf("TokensUsed = %d, want 0", resp.TokensUsed)
	}
}

func TestAdminClient_DeleteVirtualKey(t *testing.T) {
	keyID := "44444444-4444-4444-4444-444444444444"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		expectedPath := "/api/virtual-keys/" + keyID
		if r.URL.Path != expectedPath {
			t.Errorf("expected %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	if err := admin.DeleteVirtualKey(keyID); err != nil {
		t.Fatalf("DeleteVirtualKey() error: %v", err)
	}
}

func TestAdminClient_UpdateSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/settings" {
			t.Errorf("expected /api/settings, got %s", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if body["rate_limit_enabled"] != "true" {
			t.Errorf("expected rate_limit_enabled=true, got %s", body["rate_limit_enabled"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	err := admin.UpdateSettings(map[string]string{
		"rate_limit_enabled": "true",
		"rate_limit_rps":     "10",
		"rate_limit_burst":   "20",
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error: %v", err)
	}
}

func TestAdminClient_GetSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/settings" {
			t.Errorf("expected /api/settings, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"rate_limit_enabled": "false",
			"rate_limit_rps":     "0",
		})
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	settings, err := admin.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() error: %v", err)
	}
	if settings["rate_limit_enabled"] != "false" {
		t.Errorf("expected rate_limit_enabled=false, got %s", settings["rate_limit_enabled"])
	}
}

func TestAdminClient_TriggerDiscovery(t *testing.T) {
	providerID := "55555555-5555-5555-5555-555555555555"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		expectedPath := "/api/providers/" + providerID + "/discover"
		if r.URL.Path != expectedPath {
			t.Errorf("expected %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	if err := admin.TriggerDiscovery(providerID); err != nil {
		t.Fatalf("TriggerDiscovery() error: %v", err)
	}
}

func TestAdminClient_TriggerDiscovery_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	admin := NewAdminClient(srv.URL, "test-admin-token")
	err := admin.TriggerDiscovery("some-id")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// --- ProxyClient tests ---

func TestProxyClient_SendChatCompletion_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-vk-key" {
			t.Errorf("expected Authorization header, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   "mock-model",
			"choices": []interface{}{},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		})
	}))
	defer srv.Close()

	client := NewProxyClient(srv.URL, 10*time.Second)
	result := client.SendChatCompletion("test-vk-key", "mock-model", false)

	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
}

func TestProxyClient_SendChatCompletion_Streaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send a minimal SSE stream
		fmt.Fprintf(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := NewProxyClient(srv.URL, 10*time.Second)
	result := client.SendChatCompletion("test-vk-key", "mock-model", true)

	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.TTFT <= 0 {
		t.Error("TTFT should be > 0 for streaming")
	}
}

func TestProxyClient_SendChatCompletion_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	client := NewProxyClient(srv.URL, 10*time.Second)
	result := client.SendChatCompletion("test-vk-key", "mock-model", false)

	if result.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", result.StatusCode)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestProxyClient_SendChatCompletion_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	client := NewProxyClient(srv.URL, 10*time.Second)
	result := client.SendChatCompletion("test-vk-key", "mock-model", false)

	if result.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", result.StatusCode)
	}
	if result.Error == "" || result.Error[:7] != "HTTP 42" {
		t.Errorf("Error = %q, want rate limit error", result.Error)
	}
}

func TestProxyClient_SendChatCompletion_ConnectionError(t *testing.T) {
	// Use a port that's not listening
	client := NewProxyClient("http://127.0.0.1:1", 1*time.Second)
	result := client.SendChatCompletion("test-key", "model", false)

	if result.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0 for connection error", result.StatusCode)
	}
	if result.Error == "" {
		t.Error("expected non-empty error for connection failure")
	}
}

func TestProxyClient_SendChatCompletion_ExtraParams(t *testing.T) {
	var receivedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "test",
			"object": "chat.completion",
			"usage":  map[string]int{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		})
	}))
	defer srv.Close()

	client := NewProxyClient(srv.URL, 10*time.Second)
	client.ExtraParams = map[string]interface{}{
		"top_p":             0.5,
		"frequency_penalty": 1.0,
	}

	result := client.SendChatCompletion("test-key", "mock-model", false)
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}

	// Verify extra params were merged
	if topP, ok := receivedBody["top_p"]; !ok || topP != 0.5 {
		t.Errorf("top_p = %v, want 0.5", topP)
	}
	if freqPen, ok := receivedBody["frequency_penalty"]; !ok || freqPen != 1.0 {
		t.Errorf("frequency_penalty = %v, want 1.0", freqPen)
	}
}
