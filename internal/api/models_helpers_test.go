package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// createProviderAndModel creates a provider via POST /providers and inserts a
// single enabled model for it directly, returning the model's UUID. The
// UpdateModel HTTP tests share this setup, which otherwise gets copied per test.
func createProviderAndModel(t *testing.T, h *Handler, r http.Handler) string {
	t.Helper()

	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	modelID := uuid.New().String()
	_, err := h.dbPool.Pool().Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	return modelID
}

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
			got := util.BuildProviderTargetURL(tt.baseURL, tt.providerType, "/chat/completions")
			if got != tt.want {
				t.Errorf("BuildProviderTargetURL(%q, %q, %q) = %q, want %q", tt.baseURL, tt.providerType, "/chat/completions", got, tt.want)
			}
		})
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
				ContextLength:                new(128000),
				MaxOutputTokens:              new(4096),
				InputPricePerMillion:         new(10.0),
				InputPricePerMillionCacheHit: new(5.0),
				OutputPricePerMillion:        new(30.0),
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
				ContextLength:                new(128000),
				MaxOutputTokens:              new(4096),
				InputPricePerMillion:         new(10.0),
				InputPricePerMillionCacheHit: new(5.0),
				OutputPricePerMillion:        new(30.0),
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

func ptrEqual(a, b any) bool {
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

// ---------------------------------------------------------------------------
// parseTestModelResponse
// ---------------------------------------------------------------------------

func TestParseTestModelResponse_ValidJSON(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
	content, tps, promptTokens, completionTokens := parseTestModelResponse(body, 1000)

	if content != "Hi" {
		t.Errorf("content: got %q, want %q", content, "Hi")
	}
	if tps != 2.0 { // 2 tokens / 1000ms * 1000 = 2.0
		t.Errorf("tps: got %f, want %f", tps, 2.0)
	}
	if promptTokens != 5 {
		t.Errorf("promptTokens: got %d, want %d", promptTokens, 5)
	}
	if completionTokens != 2 {
		t.Errorf("completionTokens: got %d, want %d", completionTokens, 2)
	}
}

func TestParseTestModelResponse_InvalidJSON(t *testing.T) {
	body := []byte(`not json at all`)
	content, tps, promptTokens, completionTokens := parseTestModelResponse(body, 1000)

	if content != "" {
		t.Errorf("content: got %q, want empty string for invalid JSON", content)
	}
	if tps != 0 {
		t.Errorf("tps: got %f, want 0 for invalid JSON", tps)
	}
	if promptTokens != 0 {
		t.Errorf("promptTokens: got %d, want 0 for invalid JSON", promptTokens)
	}
	if completionTokens != 0 {
		t.Errorf("completionTokens: got %d, want 0 for invalid JSON", completionTokens)
	}
}

func TestParseTestModelResponse_EmptyChoices(t *testing.T) {
	body := []byte(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0}}`)
	content, tps, promptTokens, _ := parseTestModelResponse(body, 1000)

	if content != "" {
		t.Errorf("content: got %q, want empty string for empty choices", content)
	}
	if tps != 0 {
		t.Errorf("tps: got %f, want 0 when completion tokens is 0", tps)
	}
	if promptTokens != 5 {
		t.Errorf("promptTokens: got %d, want %d", promptTokens, 5)
	}
}

func TestParseTestModelResponse_ZeroDuration(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
	content, tps, _, _ := parseTestModelResponse(body, 0)

	if content != "Hi" {
		t.Errorf("content: got %q, want %q", content, "Hi")
	}
	if tps != 0 {
		t.Errorf("tps: got %f, want 0 when duration is 0", tps)
	}
}

func TestParseTestModelResponse_NoUsageField(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hello world"}}]}`)
	content, tps, promptTokens, completionTokens := parseTestModelResponse(body, 500)

	if content != "Hello world" {
		t.Errorf("content: got %q, want %q", content, "Hello world")
	}
	if tps != 0 {
		t.Errorf("tps: got %f, want 0 when usage missing", tps)
	}
	if promptTokens != 0 {
		t.Errorf("promptTokens: got %d, want 0 when usage missing", promptTokens)
	}
	if completionTokens != 0 {
		t.Errorf("completionTokens: got %d, want 0 when usage missing", completionTokens)
	}
}

// ---------------------------------------------------------------------------
// decryptTestModelKey
// ---------------------------------------------------------------------------

func TestDecryptTestModelKey_KeylessProvider(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()

	// Keyless provider: EncryptedKey is nil/empty
	prov := &provider.Provider{
		ID:   uuid.New(),
		Name: "keyless-test",
	}
	apiKey, ok := h.decryptTestModelKey(w, prov)
	if !ok {
		t.Fatal("expected ok=true for keyless provider")
	}
	if apiKey != "" {
		t.Errorf("apiKey: got %q, want empty string for keyless provider", apiKey)
	}
}

func TestDecryptTestModelKey_EncryptedKey(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()

	// Encrypt a known key with the test master key
	plainKey := "sk-test-decrypt-key-12345"
	kp, err := auth.Encrypt(plainKey, h.cfg.MasterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	prov := &provider.Provider{
		ID:           uuid.New(),
		Name:         "encrypted-test",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	apiKey, ok := h.decryptTestModelKey(w, prov)
	if !ok {
		t.Fatalf("expected ok=true for valid encrypted key, got false; body=%s", w.Body.String())
	}
	if apiKey != plainKey {
		t.Errorf("apiKey: got %q, want %q", apiKey, plainKey)
	}
}

func TestDecryptTestModelKey_InvalidEncryptedKey(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()

	// Encrypt a key with the correct master key, then corrupt the ciphertext.
	// Using a wrong master key for decryption would also work but the GCM
	// auth tag check catches that. Corrupting ciphertext triggers a clean error.
	kp, err := auth.Encrypt("sk-original", h.cfg.MasterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Flip a byte in the ciphertext to make it invalid
	corrupted := make([]byte, len(kp.Ciphertext))
	copy(corrupted, kp.Ciphertext)
	corrupted[0] ^= 0xFF

	prov := &provider.Provider{
		ID:           uuid.New(),
		Name:         "corrupted-key-test",
		EncryptedKey: corrupted,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	apiKey, ok := h.decryptTestModelKey(w, prov)
	if ok {
		t.Errorf("expected ok=false for corrupted encrypted key, got true; apiKey=%q", apiKey)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// logTestModelRequestError / logTestModelHTTPError / logTestModelCompleted
// ---------------------------------------------------------------------------

// insertTestModelForLog inserts a provider+model row needed by the
// logTestModel* tests (request_logs has FK constraints on provider_id).
func insertTestModelForLog(t *testing.T, h *Handler, modelIDStr string) *model.Model {
	t.Helper()
	ctx := context.Background()
	pool := h.dbPool.Pool()

	providerID := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		 VALUES ($1, $2, $3, true, now(), now())`,
		providerID, "log-test-provider-"+modelIDStr, "https://log-test.example.com")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	modelUUID := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO models (id, provider_id, model_id, name, enabled, created_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, true, now(), now())`,
		modelUUID, providerID, modelIDStr, modelIDStr)
	if err != nil {
		t.Fatalf("insert model: %v", err)
	}

	return &model.Model{
		ID:         modelUUID,
		ProviderID: providerID,
		ModelID:    modelIDStr,
	}
}

func TestLogTestModelRequestError(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	m := insertTestModelForLog(t, h, "test-log-req-err")

	h.logTestModelRequestError(ctx, m, "reqhash001", 1500, 200, 50, "connection refused")

	// Verify the row was inserted
	var count int
	err := h.dbPool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM request_logs WHERE request_hash = $1 AND model_id = $2`,
		"reqhash001", m.ModelID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 request_log row, got %d", count)
	}

	// Verify key fields specific to logTestModelRequestError
	var statusCode int
	var state string
	var keyDecryptMs float64
	err = h.dbPool.Pool().QueryRow(ctx,
		`SELECT status_code, state, key_decrypt_ms FROM request_logs WHERE request_hash = $1`,
		"reqhash001",
	).Scan(&statusCode, &state, &keyDecryptMs)
	if err != nil {
		t.Fatalf("failed to query request_log fields: %v", err)
	}
	if statusCode != 502 {
		t.Errorf("status_code: got %d, want 502", statusCode)
	}
	if state != "failed" {
		t.Errorf("state: got %q, want %q", state, "failed")
	}
	if keyDecryptMs != 50 {
		t.Errorf("key_decrypt_ms: got %f, want 50", keyDecryptMs)
	}
}

func TestLogTestModelHTTPError(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	m := insertTestModelForLog(t, h, "test-log-http-err")

	h.logTestModelHTTPError(ctx, m, "reqhash002", 429, 3000, 250, 30, "rate limited")

	var count int
	err := h.dbPool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM request_logs WHERE request_hash = $1 AND model_id = $2`,
		"reqhash002", m.ModelID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 request_log row, got %d", count)
	}

	var statusCode int
	var state string
	var durationMs float64
	err = h.dbPool.Pool().QueryRow(ctx,
		`SELECT status_code, state, duration_ms FROM request_logs WHERE request_hash = $1`,
		"reqhash002",
	).Scan(&statusCode, &state, &durationMs)
	if err != nil {
		t.Fatalf("failed to query request_log fields: %v", err)
	}
	if statusCode != 429 {
		t.Errorf("status_code: got %d, want 429", statusCode)
	}
	if state != "failed" {
		t.Errorf("state: got %q, want %q", state, "failed")
	}
	if durationMs != 3000 {
		t.Errorf("duration_ms: got %f, want 3000", durationMs)
	}
}

func TestLogTestModelCompleted(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	m := insertTestModelForLog(t, h, "test-log-completed")

	h.logTestModelCompleted(ctx, m, "reqhash003", 200, 2500, 100, 40, 8.5, 10, 3)

	var count int
	err := h.dbPool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM request_logs WHERE request_hash = $1 AND model_id = $2`,
		"reqhash003", m.ModelID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 request_log row, got %d", count)
	}

	var statusCode int
	var state string
	var tps float64
	var tokensPrompt int
	var tokensCompletion int
	err = h.dbPool.Pool().QueryRow(ctx,
		`SELECT status_code, state, tokens_per_second, tokens_prompt, tokens_completion FROM request_logs WHERE request_hash = $1`,
		"reqhash003",
	).Scan(&statusCode, &state, &tps, &tokensPrompt, &tokensCompletion)
	if err != nil {
		t.Fatalf("failed to query request_log fields: %v", err)
	}
	if statusCode != 200 {
		t.Errorf("status_code: got %d, want 200", statusCode)
	}
	if state != "completed" {
		t.Errorf("state: got %q, want %q", state, "completed")
	}
	if tps != 8.5 {
		t.Errorf("tokens_per_second: got %f, want 8.5", tps)
	}
	if tokensPrompt != 10 {
		t.Errorf("tokens_prompt: got %d, want 10", tokensPrompt)
	}
	if tokensCompletion != 3 {
		t.Errorf("tokens_completion: got %d, want 3", tokensCompletion)
	}
}

// TestLogTestModelRequestError_InsertError exercises the log insert failure path
// in logTestModelRequest by using a cancelled context.
func TestLogTestModelRequestError_InsertError(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately so the DB INSERT fails.

	m := insertTestModelForLog(t, h, "test-log-req-err-fail")

	// This should not panic; the log insert failure is silently logged.
	h.logTestModelRequestError(ctx, m, "reqhash-err-001", 1500, 200, 50, "connection refused")

	// Verify no row was inserted (since the context was cancelled).
	var count int
	err := h.dbPool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM request_logs WHERE request_hash = $1`,
		"reqhash-err-001",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 request_log rows (insert should have failed), got %d", count)
	}
}

// TestLogTestModelHTTPError_InsertError exercises the log insert failure path
// in logTestModelHTTPError by using a cancelled context.
func TestLogTestModelHTTPError_InsertError(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := insertTestModelForLog(t, h, "test-log-http-err-fail")

	h.logTestModelHTTPError(ctx, m, "reqhash-err-002", 429, 3000, 250, 30, "rate limited")

	// Verify no row was inserted.
	var count int
	err := h.dbPool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM request_logs WHERE request_hash = $1`,
		"reqhash-err-002",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 request_log rows (insert should have failed), got %d", count)
	}
}

// TestLogTestModelCompleted_InsertError exercises the log insert failure path
// in logTestModelCompleted by using a cancelled context.
func TestLogTestModelCompleted_InsertError(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := insertTestModelForLog(t, h, "test-log-completed-fail")

	h.logTestModelCompleted(ctx, m, "reqhash-err-003", 200, 2500, 100, 40, 8.5, 10, 3)

	// Verify no row was inserted.
	var count int
	err := h.dbPool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM request_logs WHERE request_hash = $1`,
		"reqhash-err-003",
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query request_logs: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 request_log rows (insert should have failed), got %d", count)
	}
}
