package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// ---------------------------------------------------------------------------
// discoverZAICoding — catalog-based discovery (no HTTP needed)
// ---------------------------------------------------------------------------

func zaiMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"glm-5.1","object":"model","owned_by":"z-ai"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestDiscoverZAICoding_ReturnsModels(t *testing.T) {
	server := zaiMockServer(t)
	defer server.Close()

	service := &DiscoveryService{
		httpClient: &http.Client{Transport: &testTransport{url: server.URL}},
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-zai",
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	models, err := service.discoverZAICoding(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}
	if len(models) == 0 {
		t.Error("Expected at least one model from zai-coding discovery")
	}

	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("ProviderID = %v, want %v", m.ProviderID, provider.ID)
		}
		if m.ModelID == "" {
			t.Error("ModelID should not be empty")
		}
		if m.OwnedBy != "zhipu" {
			t.Errorf("OwnedBy = %q, want %q", m.OwnedBy, "zhipu")
		}
		if !m.Enabled {
			t.Error("Expected model to be enabled")
		}
		if m.ContextLength == nil || *m.ContextLength <= 0 {
			t.Errorf("ContextLength should be > 0, got %v", m.ContextLength)
		}
		if m.MaxOutputTokens == nil || *m.MaxOutputTokens <= 0 {
			t.Errorf("MaxOutputTokens should be > 0, got %v", m.MaxOutputTokens)
		}
	}
}

func TestZAICodingSpecToModel_PriceOverrides(t *testing.T) {
	in, cacheHit, out := 2.2, 0.45, 8.9
	spec := ZAICodingModelSpec{
		ModelID:                      "glm-test",
		ContextLength:                128000,
		MaxOutputTokens:              98304,
		Modality:                     "text",
		InputPricePerMillion:         &in,
		InputPricePerMillionCacheHit: &cacheHit,
		OutputPricePerMillion:        &out,
	}
	m := zaiCodingSpecToModel(spec, uuid.New())
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 2.2 {
		t.Errorf("InputPricePerMillion = %v, want 2.2", m.InputPricePerMillion)
	}
	if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != 0.45 {
		t.Errorf("InputPricePerMillionCacheHit = %v, want 0.45", m.InputPricePerMillionCacheHit)
	}
	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 8.9 {
		t.Errorf("OutputPricePerMillion = %v, want 8.9", m.OutputPricePerMillion)
	}
	// The model must not alias the spec's pointers: the embedded catalog is
	// package-global shared state.
	if m.InputPricePerMillion == spec.InputPricePerMillion {
		t.Error("InputPricePerMillion aliases the catalog spec pointer")
	}
}

func TestZAICodingSpecToModel_NoPriceOverridesLeavesNil(t *testing.T) {
	spec := ZAICodingModelSpec{ModelID: "glm-test", ContextLength: 1, MaxOutputTokens: 1, Modality: "text"}
	m := zaiCodingSpecToModel(spec, uuid.New())
	if m.InputPricePerMillion != nil || m.OutputPricePerMillion != nil || m.InputPricePerMillionCacheHit != nil {
		t.Errorf("expected nil price fields, got in=%v cache=%v out=%v",
			m.InputPricePerMillion, m.InputPricePerMillionCacheHit, m.OutputPricePerMillion)
	}
}

// TestZAICatalog_PriceOverridesPresent pins the embedded catalog's price
// overrides: only models the canonical models.dev "zai" entry does not carry
// (glm-4.5-x, glm-4.5-airx). Models covered by models.dev must NOT duplicate
// prices here (a stale duplicate would win over fresh models.dev data), so
// glm-5.2 doubles as the no-override control. glm-5.3 deliberately carries no
// price either: its official price is unpublished, and an empty field lets
// models.dev fill it — and price-follows-source propagate it — the moment the
// real price is listed, instead of a catalog guess enforcing itself forever.
func TestZAICatalog_PriceOverridesPresent(t *testing.T) {
	specs := GetZAICodingModels()
	byID := make(map[string]ZAICodingModelSpec, len(specs))
	for _, s := range specs {
		byID[s.ModelID] = s
	}

	wantPriced := map[string][3]float64{
		"glm-4.5-x":    {2.2, 0.45, 8.9},
		"glm-4.5-airx": {1.1, 0.22, 4.5},
	}
	for id, want := range wantPriced {
		s, ok := byID[id]
		if !ok {
			t.Errorf("catalog is missing %s", id)
			continue
		}
		if s.InputPricePerMillion == nil || *s.InputPricePerMillion != want[0] {
			t.Errorf("%s InputPricePerMillion = %v, want %v", id, s.InputPricePerMillion, want[0])
		}
		if s.InputPricePerMillionCacheHit == nil || *s.InputPricePerMillionCacheHit != want[1] {
			t.Errorf("%s InputPricePerMillionCacheHit = %v, want %v", id, s.InputPricePerMillionCacheHit, want[1])
		}
		if s.OutputPricePerMillion == nil || *s.OutputPricePerMillion != want[2] {
			t.Errorf("%s OutputPricePerMillion = %v, want %v", id, s.OutputPricePerMillion, want[2])
		}
	}

	for _, id := range []string{"glm-5.2", "glm-5.3"} {
		control, ok := byID[id]
		if !ok {
			t.Fatalf("catalog is missing %s", id)
		}
		if control.InputPricePerMillion != nil || control.OutputPricePerMillion != nil {
			t.Errorf("%s must not carry a catalog price: models.dev's canonical zai entry is its price source", id)
		}
	}
}

func TestDiscoverZAICoding_VisionModelCapabilities(t *testing.T) {
	server := zaiMockServer(t)
	defer server.Close()

	service := &DiscoveryService{
		httpClient: &http.Client{Transport: &testTransport{url: server.URL}},
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-zai",
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	models, err := service.discoverZAICoding(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}

	// Find a vision model from the catalog
	found := false
	for _, m := range models {
		if m.Modality == "vision" {
			found = true
			var caps model.Capability
			if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
				t.Fatalf("Failed to unmarshal capabilities: %v", err)
			}
			if !caps.Vision {
				t.Errorf("Expected Vision capability for model %q with modality=vision", m.ModelID)
			}
			if !caps.VideoInput {
				t.Errorf("Expected VideoInput capability for model %q with modality=vision", m.ModelID)
			}
			if !strings.Contains(m.InputModalities, "image") {
				t.Errorf("Expected InputModalities to contain 'image' for vision model %q, got %q", m.ModelID, m.InputModalities)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected a vision model in the merged zai-coding result")
	}
}

// TestDiscoverZAICoding_LiveFailureAborts verifies that when the live /models
// endpoint errors, discovery aborts (returns an error) rather than falling back
// to the catalog — so RecordMissingModels never runs and existing models are
// preserved instead of having live-only models disabled.
func TestDiscoverZAICoding_LiveFailureAborts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: server.URL}}}
	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-zai",
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	models, err := svc.discoverZAICoding(context.Background(), provider, "test-key")
	if err == nil {
		t.Fatal("expected an error when live /models fails (abort, no catalog fallback)")
	}
	if models != nil {
		t.Errorf("expected nil models on abort, got %d", len(models))
	}
}

// TestDiscoverZAICoding_LiveOnlyModelPassesThrough verifies a model the live
// API lists but the catalog does not know about still surfaces.
func TestDiscoverZAICoding_LiveOnlyModelPassesThrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-future-unlisted","object":"model","owned_by":"z-ai"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: server.URL}}}
	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-zai",
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	models, err := svc.discoverZAICoding(context.Background(), provider, "test-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}
	var foundLiveOnly bool
	for _, m := range models {
		if m.ModelID == "glm-future-unlisted" {
			foundLiveOnly = true
			if m.OwnedBy != "zhipu" {
				t.Errorf("live-only OwnedBy = %q, want zhipu", m.OwnedBy)
			}
		}
	}
	if !foundLiveOnly {
		t.Error("expected live-only model to pass through discovery")
	}
}

// ---------------------------------------------------------------------------
// GetZAICodingQuota — additional error paths not covered in discovery_http_test.go
// ---------------------------------------------------------------------------

func TestGetZAICodingQuota_DecryptionError(t *testing.T) {
	service := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	// Create provider with invalid encrypted key
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai",
		EncryptedKey: []byte("invalid-encrypted-key"),
		KeyNonce:     make([]byte, 12),
		KeySalt:      make([]byte, 32),
	}

	_, err := service.GetZAICodingQuota(context.Background(), provider, "wrong-master-key")
	if err == nil {
		t.Fatal("Expected error for decryption failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decrypt API key") {
		t.Errorf("Expected decryption error, got: %v", err)
	}
}

func TestGetZAICodingQuota_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestGetZAICodingQuota_CircuitBreakerOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai-cb",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	// Exhaust retries to open the circuit breaker
	for range 5 {
		_, _ = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	}

	// Now the circuit breaker should be open - next call should fail with circuit breaker error
	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for circuit breaker open, got nil")
	}
}

// TestGetZAICodingQuota_Non200Status_ZAI tests the non-200 status code path in
// GetZAICodingQuota (line 89-93 of discovery_zai.go). A server returning 403
// (non-retryable) should cause the function to return an error containing
// "unexpected status code". Note: 429 is retryable, so doQuotaRequestWithRetry
// would retry it; 403 is non-retryable, so it returns the response and
// GetZAICodingQuota can check the status code.
func TestGetZAICodingQuota_Non200Status_ZAI(t *testing.T) {
	// A non-auth, non-retryable status (400) exercises the generic "unexpected
	// status code" path. 401/403 are classified separately as key-invalid - see
	// TestGetZAICodingQuota_KeyInvalid_ZAI.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai-rate",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("Expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status code") {
		t.Errorf("Expected 'unexpected status code' in error, got: %v", err)
	}
}

// TestGetZAICodingQuota_KeyInvalid_ZAI verifies an upstream auth rejection
// (403) is classified as ErrProviderKeyInvalid, not a generic failure, so the
// handler can answer 424 + WARN instead of 500 + ERROR.
func TestGetZAICodingQuota_KeyInvalid_ZAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": "forbidden"}`))
	}))
	defer server.Close()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("revoked-key", masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai-rate",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if !errors.Is(err, ErrProviderKeyInvalid) {
		t.Fatalf("expected ErrProviderKeyInvalid, got %v", err)
	}
}

// TestGetZAICodingQuota_JSONDecodeError_ZAI tests the JSON decode error path in
// GetZAICodingQuota (line 96-99 of discovery_zai.go). A server returning
// 200 with invalid JSON should cause a decode error.
func TestGetZAICodingQuota_JSONDecodeError_ZAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-valid-json`))
	}))
	defer server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai-decode",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("Expected error for JSON decode failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' in error, got: %v", err)
	}
}
