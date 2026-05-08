package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// Test LoadModelsDevWithClient with mock server
func TestLoadModelsDevWithClient(t *testing.T) {
	mockResponse := `{
		"provider1": {
			"id": "provider1",
			"name": "Test Provider",
			"api": "test",
			"doc": "test",
			"models": {
				"gpt-4": {
					"id": "gpt-4",
					"name": "GPT-4",
					"family": "gpt-4",
					"attachment": true,
					"reasoning": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image"], "output": ["text"]},
					"open_weights": false,
					"cost": {"input": 0.03, "output": 0.06},
					"limit": {"context": 8192, "output": 4096}
				},
				"gpt-3.5-turbo": {
					"id": "gpt-3.5-turbo",
					"name": "GPT-3.5 Turbo",
					"family": "gpt-3.5",
					"attachment": false,
					"reasoning": false,
					"tool_call": false,
					"modalities": {"input": ["text"], "output": ["text"]},
					"open_weights": false,
					"cost": {"input": 0.0015, "output": 0.002},
					"limit": {"context": 16384, "output": 4096}
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Create a test client that uses the mock server
	httpClient := server.Client()
	httpClient.Transport = &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == modelsDevAPIURL {
			return http.Get(server.URL + "/api.json")
		}
		return nil, nil
	}}

	ctx := context.Background()
	err := LoadModelsDevWithClient(ctx, httpClient)
	if err != nil {
		t.Fatalf("LoadModelsDevWithClient failed: %v", err)
	}

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("GetModelsDevCache returned nil")
	}

	// Test Lookup
	spec := cache.Lookup("gpt-4")
	if spec == nil {
		t.Fatal("Lookup failed for gpt-4")
	}
	if spec.Name != "GPT-4" {
		t.Errorf("expected name 'GPT-4', got '%s'", spec.Name)
	}

	// Test LookupFuzzy
	specFuzzy := cache.LookupFuzzy("gpt-4-2024-01-01")
	if specFuzzy == nil {
		t.Fatal("LookupFuzzy failed for gpt-4-2024-01-01")
	}
	if specFuzzy.Name != "GPT-4" {
		t.Errorf("expected name 'GPT-4' from fuzzy lookup, got '%s'", specFuzzy.Name)
	}

	// Test with unknown model
	unknown := cache.Lookup("unknown-model")
	if unknown != nil {
		t.Error("expected nil for unknown model")
	}
}

// Test UnmarshalJSON for ModelsDevInterleaved
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.roundTripFunc != nil {
		return m.roundTripFunc(req)
	}
	return nil, nil
}

func TestModelsDevInterleavedUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBool  bool
		wantField string
	}{
		{"bool true", `true`, true, ""},
		{"bool false", `false`, false, ""},
		{"object with field", `{"field":"custom"}`, true, "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inter ModelsDevInterleaved
			err := json.Unmarshal([]byte(tt.input), &inter)
			if err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if inter.Bool != tt.wantBool {
				t.Errorf("Bool: got %v, want %v", inter.Bool, tt.wantBool)
			}
			if inter.Field != tt.wantField {
				t.Errorf("Field: got %q, want %q", inter.Field, tt.wantField)
			}
		})
	}
}

// Test EnrichModel and EnrichModels
func TestModelsDevCacheEnrichModel(t *testing.T) {
	mockResponse := `{
		"provider1": {
			"id": "provider1",
			"name": "Test Provider",
			"api": "test",
			"doc": "test",
			"models": {
				"gpt-4": {
					"id": "gpt-4",
					"name": "GPT-4",
					"family": "gpt-4",
					"attachment": true,
					"reasoning": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image"], "output": ["text"]},
					"open_weights": false,
					"cost": {"input": 0.03, "output": 0.06},
					"limit": {"context": 8192, "output": 4096}
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Transport = &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == modelsDevAPIURL {
			return http.Get(server.URL + "/api.json")
		}
		return nil, nil
	}}

	ctx := context.Background()
	err := LoadModelsDevWithClient(ctx, httpClient)
	if err != nil {
		t.Fatalf("LoadModelsDevWithClient failed: %v", err)
	}

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("GetModelsDevCache returned nil")
	}

	// Test EnrichModel
	m := &model.Model{
		ID:               uuid.New(),
		ProviderID:       uuid.New(),
		ModelID:          "gpt-4",
		Name:             "gpt-4",
		DisplayName:      "gpt-4",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		OwnedBy:          "",
		Enabled:          true,
	}

	enriched := cache.EnrichModel(m)
	if !enriched {
		t.Error("expected EnrichModel to return true")
	}

	if m.DisplayName != "GPT-4" {
		t.Errorf("expected DisplayName 'GPT-4', got '%s'", m.DisplayName)
	}

	if m.ContextLength == nil || *m.ContextLength != 8192 {
		t.Errorf("expected ContextLength 8192, got %v", m.ContextLength)
	}

	if m.MaxOutputTokens == nil || *m.MaxOutputTokens != 4096 {
		t.Errorf("expected MaxOutputTokens 4096, got %v", m.MaxOutputTokens)
	}

	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 0.03 {
		t.Errorf("expected InputPricePerMillion 0.03, got %v", m.InputPricePerMillion)
	}

	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 0.06 {
		t.Errorf("expected OutputPricePerMillion 0.06, got %v", m.OutputPricePerMillion)
	}

	if m.OwnedBy != "gpt-4" {
		t.Errorf("expected OwnedBy 'gpt-4', got '%s'", m.OwnedBy)
	}

	// Test EnrichModels
	models := []*model.Model{
		{
			ID:               uuid.New(),
			ProviderID:       uuid.New(),
			ModelID:          "gpt-4",
			Name:             "gpt-4",
			DisplayName:      "gpt-4",
			Capabilities:     "{}",
			Params:           "{}",
			Modality:         "text",
			InputModalities:  "[]",
			OutputModalities: "[]",
			OwnedBy:          "",
			Enabled:          true,
		},
		{
			ID:               uuid.New(),
			ProviderID:       uuid.New(),
			ModelID:          "unknown-model",
			Name:             "unknown",
			DisplayName:      "unknown",
			Capabilities:     "{}",
			Params:           "{}",
			Modality:         "text",
			InputModalities:  "[]",
			OutputModalities: "[]",
			OwnedBy:          "",
			Enabled:          true,
		},
	}

	enrichedCount := cache.EnrichModels(models)
	if enrichedCount != 1 {
		t.Errorf("expected EnrichModels to enrich 1 model, got %d", enrichedCount)
	}
}
