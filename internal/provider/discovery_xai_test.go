package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// isNoAccessError
// ---------------------------------------------------------------------------

func TestIsNoAccessError_Nil(t *testing.T) {
	if isNoAccessError(nil) {
		t.Error("isNoAccessError(nil) = true, want false")
	}
}

func TestIsNoAccessError_Forbidden(t *testing.T) {
	err := &httpError{StatusCode: http.StatusForbidden}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(403) = false, want true")
	}
}

func TestIsNoAccessError_TooManyRequests(t *testing.T) {
	err := &httpError{StatusCode: http.StatusTooManyRequests}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(429) = false, want true")
	}
}

func TestIsNoAccessError_Unauthorized(t *testing.T) {
	err := &httpError{StatusCode: http.StatusUnauthorized}
	if isNoAccessError(err) {
		t.Error("isNoAccessError(401) = true, want false")
	}
}

func TestIsNoAccessError_InternalServerError(t *testing.T) {
	err := &httpError{StatusCode: http.StatusInternalServerError}
	if isNoAccessError(err) {
		t.Error("isNoAccessError(500) = true, want false")
	}
}

func TestIsNoAccessError_OtherErrorType(t *testing.T) {
	err := http.ErrAbortHandler
	if isNoAccessError(err) {
		t.Error("isNoAccessError(non-httpError) = true, want false")
	}
}

func TestIsNoAccessError_Pointer(t *testing.T) {
	err := &httpError{StatusCode: http.StatusForbidden}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(&httpError{403}) = false, want true")
	}
}

func TestHTTPError_Error(t *testing.T) {
	err := &httpError{StatusCode: 418, Body: "I'm a teapot"}
	msg := err.Error()
	if msg != "unexpected status 418" {
		t.Errorf("Error() = %q, want %q", msg, "unexpected status 418")
	}
}

// ---------------------------------------------------------------------------
// discoverXAI catalog fallback (lines 41-44)
// ---------------------------------------------------------------------------

func TestDiscoverXAI_CatalogFallbackAfter403(t *testing.T) {
	ctx := t.Context()

	// Create a test server that returns 403 on both endpoints
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/language-models", "/language-models":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error": "forbidden"}`))
		case "/v1/models", "/models":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error": "forbidden"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	disc := &DiscoveryService{httpClient: server.Client()}
	models, err := disc.discoverXAI(ctx, provider, "test-api-key")

	if err != nil {
		t.Fatalf("discoverXAI() returned error: %v", err)
	}

	// Should fall back to catalog
	if len(models) == 0 {
		t.Error("discoverXAI() returned 0 models, expected catalog models")
	}

	// Verify catalog models have expected fields
	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("model.ProviderID = %v, want %v", m.ProviderID, provider.ID)
		}
		if !m.Enabled {
			t.Error("model.Enabled = false, want true")
		}
	}
}

// ---------------------------------------------------------------------------
// discoverXAIMinimalModels 403 handling (lines 187-189)
// ---------------------------------------------------------------------------

func TestDiscoverXAIMinimalModels_403ReturnsHttpError(t *testing.T) {
	ctx := t.Context()

	// Create a test server that returns 403 on /models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error": "forbidden"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	disc := &DiscoveryService{httpClient: server.Client()}
	models, err := disc.discoverXAIMinimalModels(ctx, provider, "test-api-key", server.URL)

	// Should return nil models and httpError
	if models != nil {
		t.Errorf("discoverXAIMinimalModels() returned %d models, want nil", len(models))
	}

	if err == nil {
		t.Fatal("discoverXAIMinimalModels() returned nil error, expected httpError")
	}

	// Verify it's an httpError with 403 status
	httpErr := &httpError{}
	if !errors.As(err, &httpErr) {
		t.Fatalf("err is not httpError: %T", err)
	}
	if httpErr.StatusCode != http.StatusForbidden {
		t.Errorf("httpErr.StatusCode = %d, want %d", httpErr.StatusCode, http.StatusForbidden)
	}

	// Verify isNoAccessError returns true for this error
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(err) = false, want true for 403")
	}
}

// ---------------------------------------------------------------------------
// Catalog spec override (lines 100-102 + 141-158)
// ---------------------------------------------------------------------------

func TestDiscoverXAILanguageModels_CatalogSpecOverride(t *testing.T) {
	ctx := t.Context()

	// Use a model ID that exists in the xAI catalog
	catalogModelID := "grok-4.20-0309-reasoning"

	// Create a test server that returns a language model with incomplete data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/language-models" || r.URL.Path == "/language-models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Return model with API data that should be overridden by catalog
			_, _ = fmt.Fprintf(w, `{"models": [{
				"id": "%s",
				"fingerprint": "abc123",
				"created": 1234567890,
				"object": "language_model",
				"owned_by": "xai",
				"version": "1.0",
				"input_modalities": ["text"],
				"output_modalities": ["text"],
				"prompt_text_token_price": 200,
				"cached_prompt_text_token_price": 20,
				"completion_text_token_price": 600,
				"search_price": 0,
				"aliases": []
			}]}`, catalogModelID)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	disc := &DiscoveryService{httpClient: server.Client()}
	models, err := disc.discoverXAILanguageModels(ctx, provider, "test-api-key", server.URL)

	if err != nil {
		t.Fatalf("discoverXAILanguageModels() returned error: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("discoverXAILanguageModels() returned %d models, want 1", len(models))
	}

	m := models[0]

	// Get expected catalog spec
	catalog := GetXAICatalog()
	spec := LookupOpenCodeCatalog(catalog, catalogModelID)
	if spec == nil {
		t.Fatal("catalog spec not found for", catalogModelID)
	}

	// Verify catalog fields override API values
	if m.DisplayName != spec.DisplayName {
		t.Errorf("DisplayName = %q, want %q (from catalog)", m.DisplayName, spec.DisplayName)
	}
	if m.Description != spec.Description {
		t.Errorf("Description = %q, want %q (from catalog)", m.Description, spec.Description)
	}

	// ContextLength should be set from catalog (API doesn't provide it)
	if m.ContextLength == nil {
		t.Error("ContextLength = nil, want value from catalog")
	} else if *m.ContextLength != spec.ContextLength {
		t.Errorf("ContextLength = %d, want %d (from catalog)", *m.ContextLength, spec.ContextLength)
	}

	// MaxOutputTokens should be set from catalog (API doesn't provide it)
	if m.MaxOutputTokens == nil {
		t.Error("MaxOutputTokens = nil, want value from catalog")
	} else if *m.MaxOutputTokens != spec.MaxOutputTokens {
		t.Errorf("MaxOutputTokens = %d, want %d (from catalog)", *m.MaxOutputTokens, spec.MaxOutputTokens)
	}

	// Verify pricing was converted correctly from API (cents per 100M -> dollars per 1M)
	expectedInputPrice := 2.0  // 200 cents / 100 = 2.0 dollars per 1M
	expectedOutputPrice := 6.0 // 600 cents / 100 = 6.0 dollars per 1M
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != expectedInputPrice {
		t.Errorf("InputPricePerMillion = %v, want %v", m.InputPricePerMillion, expectedInputPrice)
	}
	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != expectedOutputPrice {
		t.Errorf("OutputPricePerMillion = %v, want %v", m.OutputPricePerMillion, expectedOutputPrice)
	}
}
