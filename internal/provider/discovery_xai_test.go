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
// Rich /language-models path returns clean live-only fields; catalog backfill
// happens later in mergeLiveAndCatalog (see catalog_merge_test.go).
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

	// This layer no longer applies the catalog: it returns only what the live
	// API provides. Display name is the raw id placeholder, description is empty
	// (no fabricated "xAI language model (vX)"), and context/max-output are nil
	// because xAI's API does not report them. mergeLiveAndCatalog backfills all
	// of these afterwards.
	if m.DisplayName != catalogModelID {
		t.Errorf("DisplayName = %q, want raw id %q (no catalog override here)", m.DisplayName, catalogModelID)
	}
	if m.Description != "" {
		t.Errorf("Description = %q, want empty (no fabricated placeholder)", m.Description)
	}
	if m.ContextLength != nil {
		t.Errorf("ContextLength = %d, want nil (backfilled later by catalog)", *m.ContextLength)
	}
	if m.MaxOutputTokens != nil {
		t.Errorf("MaxOutputTokens = %d, want nil (backfilled later by catalog)", *m.MaxOutputTokens)
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

// ---------------------------------------------------------------------------
// discoverXAI — both endpoints fail with real errors (not 403/429)
// ---------------------------------------------------------------------------

// TestDiscoverXAI_BothEndpointsInternalServerError tests that discoverXAI
// returns an error when both the language-models and models endpoints
// return internal server errors (not 403/429 which would trigger catalog fallback).
func TestDiscoverXAI_BothEndpointsInternalServerError(t *testing.T) {
	ctx := t.Context()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai-500",
		BaseURL: server.URL,
	}

	disc := &DiscoveryService{httpClient: server.Client()}
	models, err := disc.discoverXAI(ctx, p, "test-api-key")

	// Should return an error (not fall back to catalog since it's 500 not 403/429)
	if err == nil {
		t.Error("discoverXAI() expected error for 500 responses, got nil")
	}
	if models != nil {
		t.Errorf("discoverXAI() expected nil models for 500, got %d", len(models))
	}
}
