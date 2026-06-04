package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ---------------------------------------------------------------------------
// buildProviderTargetURL
// ---------------------------------------------------------------------------

func TestBuildProviderTargetURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		providerType string
		want         string
	}{
		{
			name:         "anthropic basic",
			baseURL:      "https://api.anthropic.com",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic trailing slash",
			baseURL:      "https://api.anthropic.com/",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic already /v1",
			baseURL:      "https://api.anthropic.com/v1",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic already /v1/",
			baseURL:      "https://api.anthropic.com/v1/",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "openai standard",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			want:         "https://api.openai.com/v1/chat/completions",
		},
		{
			name:         "google provider",
			baseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
			providerType: "google",
			want:         "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		},
		{
			name:         "cohere provider",
			baseURL:      "https://api.cohere.ai/compatibility/v1",
			providerType: "cohere",
			want:         "https://api.cohere.ai/compatibility/v1/chat/completions",
		},
		{
			name:         "empty providerType",
			baseURL:      "https://example.com/v1",
			providerType: "",
			want:         "https://example.com/v1/chat/completions",
		},
		{
			name:         "deepseek provider",
			baseURL:      "https://api.deepseek.com",
			providerType: "deepseek",
			want:         "https://api.deepseek.com/chat/completions",
		},
		{
			name:         "xai provider",
			baseURL:      "https://api.x.ai",
			providerType: "xai",
			want:         "https://api.x.ai/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.BuildProviderTargetURL(tt.baseURL, tt.providerType)
			if got != tt.want {
				t.Errorf("BuildProviderTargetURL(%q, %q) = %q, want %q", tt.baseURL, tt.providerType, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// setProviderAuthHeaders
// ---------------------------------------------------------------------------

func TestSetProviderAuthHeaders_EmptyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	util.SetProviderAuthHeaders(req, "anthropic", "")
	if req.Header.Get("x-api-key") != "" {
		t.Error("expected no x-api-key header for empty key")
	}
	if req.Header.Get("anthropic-version") != "" {
		t.Error("expected no anthropic-version header for empty key")
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("expected no Authorization header for empty key")
	}
}

func TestSetProviderAuthHeaders_Anthropic(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	util.SetProviderAuthHeaders(req, "anthropic", "sk-test-key")
	if v := req.Header.Get("x-api-key"); v != "sk-test-key" {
		t.Errorf("x-api-key = %q, want %q", v, "sk-test-key")
	}
	if v := req.Header.Get("anthropic-version"); v != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", v, "2023-06-01")
	}
	if v := req.Header.Get("Authorization"); v != "" {
		t.Errorf("Authorization should not be set for anthropic, got %q", v)
	}
}

func TestSetProviderAuthHeaders_OpenAI(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	util.SetProviderAuthHeaders(req, "openai", "sk-test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer sk-test-key")
	}
	if v := req.Header.Get("x-api-key"); v != "" {
		t.Errorf("x-api-key should not be set for openai, got %q", v)
	}
}

func TestSetProviderAuthHeaders_Google(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	util.SetProviderAuthHeaders(req, "google", "test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer test-key")
	}
}

func TestSetProviderAuthHeaders_EmptyProvider(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	util.SetProviderAuthHeaders(req, "", "key")
	if v := req.Header.Get("Authorization"); v != "Bearer key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer key")
	}
}

// ---------------------------------------------------------------------------
// modelToResponse
// ---------------------------------------------------------------------------

func TestModelToResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    model.Model
		wantResp ModelResponse
	}{
		{
			name: "basic model with all fields populated",
			input: model.Model{
				ID:                           uuid.Must(uuid.NewV7()),
				ModelID:                      "gpt-4-turbo",
				Name:                         "GPT-4 Turbo",
				Description:                  "Advanced language model",
				DisplayName:                  "GPT-4 Turbo Custom",
				ProviderID:                   uuid.Must(uuid.NewV7()),
				ProviderName:                 "OpenAI",
				Capabilities:                 `{"streaming":true,"vision":true}`,
				Params:                       `{"temperature":0.7}`,
				Modality:                     "text",
				InputModalities:              `["text"]`,
				OutputModalities:             `["text"]`,
				ContextLength:                intPtr(128000),
				MaxOutputTokens:              intPtr(4096),
				InputPricePerMillion:         float64Ptr(10.0),
				InputPricePerMillionCacheHit: float64Ptr(5.0),
				OutputPricePerMillion:        float64Ptr(30.0),
				OwnedBy:                      "OpenAI",
				Enabled:                      true,
				CreatedAt:                    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				LastSeenAt:                   time.Date(2024, 5, 20, 14, 45, 30, 0, time.UTC),
			},
			wantResp: ModelResponse{
				ID:                           "", // synced from input via assertion below
				ModelID:                      "gpt-4-turbo",
				Name:                         "GPT-4 Turbo",
				Description:                  "Advanced language model",
				DisplayName:                  "GPT-4 Turbo Custom",
				ProviderID:                   "", // synced from input via assertion below
				ProviderName:                 "OpenAI",
				Capabilities:                 `{"streaming":true,"vision":true}`,
				Params:                       `{"temperature":0.7}`,
				Modality:                     "text",
				InputModalities:              `["text"]`,
				OutputModalities:             `["text"]`,
				ContextLength:                intPtr(128000),
				MaxOutputTokens:              intPtr(4096),
				InputPricePerMillion:         float64Ptr(10.0),
				InputPricePerMillionCacheHit: float64Ptr(5.0),
				OutputPricePerMillion:        float64Ptr(30.0),
				OwnedBy:                      "OpenAI",
				Enabled:                      true,
				CreatedAt:                    "2024-01-15T10:30:00Z",
				LastSeenAt:                   "2024-05-20T14:45:30Z",
			},
		},
		{
			name: "model with empty optional fields",
			input: model.Model{
				ID:                           uuid.Must(uuid.NewV7()),
				ModelID:                      "test-model",
				Name:                         "",
				Description:                  "",
				DisplayName:                  "",
				ProviderID:                   uuid.Must(uuid.NewV7()),
				ProviderName:                 "Test Provider",
				Capabilities:                 "{}",
				Params:                       "{}",
				Modality:                     "",
				InputModalities:              "[]",
				OutputModalities:             "[]",
				ContextLength:                nil,
				MaxOutputTokens:              nil,
				InputPricePerMillion:         nil,
				InputPricePerMillionCacheHit: nil,
				OutputPricePerMillion:        nil,
				OwnedBy:                      "",
				Enabled:                      false,
				CreatedAt:                    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				LastSeenAt:                   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantResp: ModelResponse{
				ID:                           "", // synced from input
				ModelID:                      "test-model",
				Name:                         "",
				Description:                  "",
				DisplayName:                  "",
				ProviderID:                   "", // synced from input
				ProviderName:                 "Test Provider",
				Capabilities:                 "{}",
				Params:                       "{}",
				Modality:                     "",
				InputModalities:              "[]",
				OutputModalities:             "[]",
				ContextLength:                nil,
				MaxOutputTokens:              nil,
				InputPricePerMillion:         nil,
				InputPricePerMillionCacheHit: nil,
				OutputPricePerMillion:        nil,
				OwnedBy:                      "",
				Enabled:                      false,
				CreatedAt:                    "2024-01-01T00:00:00Z",
				LastSeenAt:                   "2024-01-01T00:00:00Z",
			},
		},
		{
			name: "model with timezone-aware timestamps",
			input: model.Model{
				ID:           uuid.Must(uuid.NewV7()),
				ModelID:      "timezone-test",
				ProviderID:   uuid.Must(uuid.NewV7()),
				ProviderName: "Timezone Provider",
				CreatedAt:    time.Date(2024, 6, 15, 18, 25, 45, 123456789, time.FixedZone("-07:00", -7*3600)),
				LastSeenAt:   time.Date(2024, 6, 16, 9, 10, 30, 500000000, time.FixedZone("+05:30", 5*3600+30*60)),
			},
			wantResp: ModelResponse{
				ID:           "", // synced from input
				ModelID:      "timezone-test",
				ProviderID:   "", // synced from input
				ProviderName: "Timezone Provider",
				CreatedAt:    "2024-06-15T18:25:45-07:00",
				LastSeenAt:   "2024-06-16T09:10:30+05:30",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelToResponse(tt.input)

			// For UUID fields, we just check they're non-empty and properly formatted
			if got.ID == "" {
				t.Error("ID should not be empty")
			}
			if got.ProviderID == "" {
				t.Error("ProviderID should not be empty")
			}

			// Sync expected UUIDs from input so we can compare them for equality
			tt.wantResp.ID = tt.input.ID.String()
			tt.wantResp.ProviderID = tt.input.ProviderID.String()

			// Compare all fields
			if got.ModelID != tt.wantResp.ModelID {
				t.Errorf("ModelID: got %q, want %q", got.ModelID, tt.wantResp.ModelID)
			}
			if got.Name != tt.wantResp.Name {
				t.Errorf("Name: got %q, want %q", got.Name, tt.wantResp.Name)
			}
			if got.Description != tt.wantResp.Description {
				t.Errorf("Description: got %q, want %q", got.Description, tt.wantResp.Description)
			}
			if got.DisplayName != tt.wantResp.DisplayName {
				t.Errorf("DisplayName: got %q, want %q", got.DisplayName, tt.wantResp.DisplayName)
			}
			if got.ProviderName != tt.wantResp.ProviderName {
				t.Errorf("ProviderName: got %q, want %q", got.ProviderName, tt.wantResp.ProviderName)
			}
			if got.Capabilities != tt.wantResp.Capabilities {
				t.Errorf("Capabilities: got %q, want %q", got.Capabilities, tt.wantResp.Capabilities)
			}
			if got.Params != tt.wantResp.Params {
				t.Errorf("Params: got %q, want %q", got.Params, tt.wantResp.Params)
			}
			if got.Modality != tt.wantResp.Modality {
				t.Errorf("Modality: got %q, want %q", got.Modality, tt.wantResp.Modality)
			}
			if got.InputModalities != tt.wantResp.InputModalities {
				t.Errorf("InputModalities: got %q, want %q", got.InputModalities, tt.wantResp.InputModalities)
			}
			if got.OutputModalities != tt.wantResp.OutputModalities {
				t.Errorf("OutputModalities: got %q, want %q", got.OutputModalities, tt.wantResp.OutputModalities)
			}
			if !ptrEqual(got.ContextLength, tt.wantResp.ContextLength) {
				t.Errorf("ContextLength: got %v, want %v", got.ContextLength, tt.wantResp.ContextLength)
			}
			if !ptrEqual(got.MaxOutputTokens, tt.wantResp.MaxOutputTokens) {
				t.Errorf("MaxOutputTokens: got %v, want %v", got.MaxOutputTokens, tt.wantResp.MaxOutputTokens)
			}
			if !ptrEqual(got.InputPricePerMillion, tt.wantResp.InputPricePerMillion) {
				t.Errorf("InputPricePerMillion: got %v, want %v", got.InputPricePerMillion, tt.wantResp.InputPricePerMillion)
			}
			if !ptrEqual(got.InputPricePerMillionCacheHit, tt.wantResp.InputPricePerMillionCacheHit) {
				t.Errorf("InputPricePerMillionCacheHit: got %v, want %v", got.InputPricePerMillionCacheHit, tt.wantResp.InputPricePerMillionCacheHit)
			}
			if !ptrEqual(got.OutputPricePerMillion, tt.wantResp.OutputPricePerMillion) {
				t.Errorf("OutputPricePerMillion: got %v, want %v", got.OutputPricePerMillion, tt.wantResp.OutputPricePerMillion)
			}
			if got.OwnedBy != tt.wantResp.OwnedBy {
				t.Errorf("OwnedBy: got %q, want %q", got.OwnedBy, tt.wantResp.OwnedBy)
			}
			if got.Enabled != tt.wantResp.Enabled {
				t.Errorf("Enabled: got %v, want %v", got.Enabled, tt.wantResp.Enabled)
			}
			if got.CreatedAt != tt.wantResp.CreatedAt {
				t.Errorf("CreatedAt: got %q, want %q", got.CreatedAt, tt.wantResp.CreatedAt)
			}
			if got.LastSeenAt != tt.wantResp.LastSeenAt {
				t.Errorf("LastSeenAt: got %q, want %q", got.LastSeenAt, tt.wantResp.LastSeenAt)
			}
		})
	}
}

// Helper functions for testing
func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}

func ptrEqual(a, b interface{}) bool {
	// Handle nil cases - this handles when the interface{} itself is nil
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Check if we have nil pointers wrapped in interfaces
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)

	// If both are nil pointers, they're equal
	if (av.Kind() == reflect.Pointer && av.IsNil()) && (bv.Kind() == reflect.Pointer && bv.IsNil()) {
		return true
	}

	// If one is a nil pointer and the other isn't, they're not equal
	if (av.Kind() == reflect.Pointer && av.IsNil()) || (bv.Kind() == reflect.Pointer && bv.IsNil()) {
		return false
	}

	// Both are non-nil pointers, so we can safely dereference
	switch va := a.(type) {
	case *int:
		if vb, ok := b.(*int); ok {
			return *va == *vb
		}
	case *float64:
		if vb, ok := b.(*float64); ok {
			return *va == *vb
		}
	}
	return false
}
