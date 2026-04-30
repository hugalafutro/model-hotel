package proxy

import (
	"context"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// ---------------------------------------------------------------------------
// shouldFailover pure unit tests (no DB required)
//
// For status codes that return before reaching settingsRepo.GetBool
// (5xx, 401/403, and non-failover codes), settingsRepo can be nil safely.
// ---------------------------------------------------------------------------

func TestShouldFailover_PureUnit_5xx(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: 5xx path returns before touching settingsRepo
	}

	for _, code := range []int{500, 501, 502, 503, 504, 505, 510, 511, 599} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_PureUnit_AuthErrors(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: auth error path returns before touching settingsRepo
	}

	for _, code := range []int{401, 403} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_PureUnit_NoFailoverCodes(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: non-failover codes return false before reaching settingsRepo
	}

	tests := []struct {
		name string
		code int
	}{
		{"200 OK", 200},
		{"201 Created", 201},
		{"204 No Content", 204},
		{"301 Moved", 301},
		{"302 Found", 302},
		{"304 Not Modified", 304},
		{"400 Bad Request", 400},
		{"404 Not Found", 404},
		{"405 Method Not Allowed", 405},
		{"408 Request Timeout", 408},
		{"415 Unsupported Media Type", 415},
		{"422 Unprocessable Entity", 422},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h.shouldFailover(context.Background(), tt.code) {
				t.Errorf("status %d should NOT trigger failover", tt.code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// shouldFailover integration tests (requires PostgreSQL)
//
// The 429 path calls settingsRepo.GetBool, so it needs a real DB.
// ---------------------------------------------------------------------------

func TestShouldFailover_Integration_429DefaultEnabled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	// Default setting for failover_on_rate_limit is true
	if !h.shouldFailover(context.Background(), 429) {
		t.Error("429 should trigger failover when failover_on_rate_limit=true (default)")
	}
}

func TestShouldFailover_Integration_429Disabled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	if err := h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "false"); err != nil {
		t.Fatalf("failed to set setting: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "true")
	}()
	h.settingsRepo.InvalidateCache("failover_on_rate_limit")

	if h.shouldFailover(context.Background(), 429) {
		t.Error("429 should NOT trigger failover when failover_on_rate_limit=false")
	}
}

func TestShouldFailover_Integration_TableDriven(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name     string
		code     int
		expected bool
	}{
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"401 Unauthorized", 401, true},
		{"403 Forbidden", 403, true},
		{"429 Too Many Requests", 429, true},
		{"200 OK", 200, false},
		{"201 Created", 201, false},
		{"400 Bad Request", 400, false},
		{"404 Not Found", 404, false},
		{"422 Unprocessable", 422, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.shouldFailover(context.Background(), tt.code)
			if got != tt.expected {
				t.Errorf("shouldFailover(%d) = %v, want %v", tt.code, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveHotelModel integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestResolveHotelModel_GroupNotFound(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	candidates, timings, err := h.resolveHotelModel(context.Background(), "nonexistent-model-xyz")

	if err == nil {
		t.Error("expected error for nonexistent failover group")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
	// Timings should be zero since the error occurs before any lookup
	if timings.modelLookupMs != 0 {
		t.Errorf("modelLookupMs = %f, want 0 on early error", timings.modelLookupMs)
	}
	if timings.providerLookupMs != 0 {
		t.Errorf("providerLookupMs = %f, want 0 on early error", timings.providerLookupMs)
	}
	if timings.keyDecryptMs != 0 {
		t.Errorf("keyDecryptMs = %f, want 0 on early error", timings.keyDecryptMs)
	}
}

func TestResolveHotelModel_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := h.resolveHotelModel(ctx, "some-model")

	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// ---------------------------------------------------------------------------
// resolveSpecificProvider integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestResolveSpecificProvider_ProviderNotFound(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	candidates, timings, err := h.resolveSpecificProvider(context.Background(), "nonexistent-provider", "some-model")

	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
	// providerLookupMs is measured even on error (time.Since was called)
	_ = timings
}

func TestResolveSpecificProvider_ModelNotFound(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	providers, err := h.providerRepo.List(context.Background())
	if err != nil || len(providers) == 0 {
		t.Skip("no providers in database")
	}

	candidates, _, err := h.resolveSpecificProvider(context.Background(), providers[0].Name, "nonexistent-model-xyz")

	if err == nil {
		t.Error("expected error for nonexistent model")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
}

func TestResolveSpecificProvider_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := h.resolveSpecificProvider(ctx, "some-provider", "some-model")

	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// ---------------------------------------------------------------------------
// resolveTimings struct tests
// ---------------------------------------------------------------------------

func TestResolveTimings_ZeroValue(t *testing.T) {
	var rt resolveTimings

	if rt.modelLookupMs != 0 {
		t.Errorf("zero resolveTimings.modelLookupMs = %f, want 0", rt.modelLookupMs)
	}
	if rt.providerLookupMs != 0 {
		t.Errorf("zero resolveTimings.providerLookupMs = %f, want 0", rt.providerLookupMs)
	}
	if rt.keyDecryptMs != 0 {
		t.Errorf("zero resolveTimings.keyDecryptMs = %f, want 0", rt.keyDecryptMs)
	}
}
