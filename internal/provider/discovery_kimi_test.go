package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// kimiUsagePayload is the real /usages response (live-captured 2026-07-19).
// Note numeric fields arrive as JSON strings.
const kimiUsagePayload = `{
	"user": {"userId": "u-1", "region": "REGION_OVERSEA", "membership": {"level": "LEVEL_BASIC"}},
	"usage": {"limit": "100", "remaining": "100", "resetTime": "2026-07-26T12:10:02Z"},
	"limits": [{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "100", "resetTime": "2026-07-19T17:10:02Z"}}],
	"parallel": {"limit": "10"},
	"totalQuota": {"limit": "100", "remaining": "99"},
	"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
	"subType": "TYPE_PURCHASE"
}`

// kimiListing is a trimmed copy of the real /coding/v1/models response
// (live-captured 2026-07-19).
const kimiListing = `{
	"object": "list",
	"data": [
		{"id": "k3", "object": "model", "created": 1761264000, "display_name": "K3", "type": "model", "context_length": 262144, "supports_reasoning": true, "supports_image_in": true, "supports_video_in": true, "supports_thinking_type": "only"},
		{"id": "kimi-for-coding", "object": "model", "created": 1761264000, "display_name": "K2.7 Coding", "type": "model", "context_length": 262144, "supports_reasoning": true, "supports_image_in": true, "supports_video_in": true, "supports_thinking_type": "only"},
		{"id": "text-only-model", "object": "model", "created": 1761264000, "context_length": 8192, "supports_reasoning": false, "supports_image_in": false, "supports_video_in": false},
		{"id": "reasoning-only-model", "object": "model", "created": 1761264000, "context_length": 4096, "supports_reasoning": true, "supports_image_in": false, "supports_video_in": false}
	]
}`

func TestDiscoverKimiCode_MapsListingMetadata(t *testing.T) {
	t.Parallel()

	apiKey := "test-api-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(kimiListing))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverKimiCode(context.Background(), provider, apiKey)
	assert.NoError(t, err)
	assert.Len(t, models, 4)

	byID := map[string]*model.Model{}
	for _, m := range models {
		byID[m.ModelID] = m
	}

	k3 := byID["k3"]
	assert.NotNil(t, k3)
	assert.Equal(t, "K3", k3.DisplayName)
	assert.Equal(t, "moonshotai", k3.OwnedBy)
	if assert.NotNil(t, k3.ContextLength) {
		assert.Equal(t, 262144, *k3.ContextLength)
	}
	assert.Contains(t, k3.Capabilities, `"streaming":true`)
	assert.Contains(t, k3.Capabilities, `"reasoning":true`)
	assert.Contains(t, k3.Capabilities, `"vision":true`)
	assert.Equal(t, `["text","image","video"]`, k3.InputModalities)
	assert.Equal(t, `["text"]`, k3.OutputModalities)
	assert.True(t, k3.Enabled)

	// Entry with no display_name and no capability flags: id fallback, text-only.
	plain := byID["text-only-model"]
	assert.NotNil(t, plain)
	assert.Equal(t, "text-only-model", plain.DisplayName)
	assert.Contains(t, plain.Capabilities, `"reasoning":false`)
	assert.Equal(t, `["text"]`, plain.InputModalities)
	if assert.NotNil(t, plain.ContextLength) {
		assert.Equal(t, 8192, *plain.ContextLength)
	}

	// Entry with reasoning but no vision: discriminates a swapped
	// Reasoning/Vision capability mapping in kimiCodeLiveModel.
	reasoningOnly := byID["reasoning-only-model"]
	assert.NotNil(t, reasoningOnly)
	assert.Contains(t, reasoningOnly.Capabilities, `"reasoning":true`)
	assert.Contains(t, reasoningOnly.Capabilities, `"vision":false`)
	assert.Equal(t, `["text"]`, reasoningOnly.InputModalities)
}

func TestDiscoverKimiCode_FetchErrorAborts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverKimiCode(context.Background(), provider, "k")
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestDiscoverKimiCode_EmptyListing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverKimiCode(context.Background(), provider, "k")
	assert.NoError(t, err)
	assert.Empty(t, models)
}

// TestKimiCodeLiveModel_ImageOnlyModality verifies the image-in-without-video
// branch of the input-modality switch in kimiCodeLiveModel, which the shared
// kimiListing fixture (image+video, or neither) doesn't exercise.
func TestKimiCodeLiveModel_ImageOnlyModality(t *testing.T) {
	t.Parallel()

	m := kimiCodeModel{
		ID:              "image-only-model",
		SupportsImageIn: true,
	}

	mm := kimiCodeLiveModel(m, uuid.New())
	assert.Equal(t, `["text","image"]`, mm.InputModalities)
}

// TestDiscoverKimiCode_DecodeError verifies that a 200 response with a body
// that isn't valid JSON surfaces a decode error instead of panicking or
// silently returning an empty model list.
func TestDiscoverKimiCode_DecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverKimiCode(context.Background(), provider, "k")
	assert.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "failed to decode models")
}

// ---------------------------------------------------------------------------
// GetKimiCodeQuota
// ---------------------------------------------------------------------------

func TestGetKimiCodeQuota_DecodesUsage(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"
	kp, err := auth.Encrypt(apiKey, masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(kimiUsagePayload))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client()}

	quota, err := svc.GetKimiCodeQuota(context.Background(), provider, masterKey)
	assert.NoError(t, err)
	assert.NotNil(t, quota)
	assert.Equal(t, "100", quota.Usage.Remaining)
	if assert.Len(t, quota.Limits, 1) {
		assert.Equal(t, 300, quota.Limits[0].Window.Duration)
	}
}

func TestGetKimiCodeQuota_KeyInvalid(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("revoked-key", masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: 0}

	_, err = svc.GetKimiCodeQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "auth") || errors.Is(err, ErrProviderKeyInvalid),
		"expected an auth-related error, got: %v", err)
}

// TestGetKimiCodeQuota_DecryptFailure verifies that a provider row with a
// key that doesn't decrypt (e.g. corrupted ciphertext/nonce/salt) fails
// fast with a decrypt error and never issues an HTTP request.
func TestGetKimiCodeQuota_DecryptFailure(t *testing.T) {
	t.Parallel()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      "https://api.kimi.com/coding/v1",
		EncryptedKey: []byte("not-real-ciphertext"),
		KeyNonce:     []byte("bad-nonce"),
		KeySalt:      []byte("bad-salt"),
	}
	svc := &DiscoveryService{httpClient: http.DefaultClient}

	_, err := svc.GetKimiCodeQuota(context.Background(), provider, "test-master-key-1234567890123456")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt API key")
}

// TestGetKimiCodeQuota_UnexpectedStatus verifies that a non-200, non-auth,
// non-retryable status code (e.g. 400) surfaces a generic "unexpected
// status code" error rather than ErrProviderKeyInvalid.
func TestGetKimiCodeQuota_UnexpectedStatus(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("test-api-key", masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: 0}

	_, err = svc.GetKimiCodeQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrProviderKeyInvalid))
	assert.Contains(t, err.Error(), "unexpected status code 400")
}

// TestGetKimiCodeQuota_RetryExhausted verifies that a persistently
// retryable status (5xx) exhausts the quota-fetch retry budget and returns
// an error from the retry loop rather than hanging or succeeding.
func TestGetKimiCodeQuota_RetryExhausted(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("test-api-key", masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: 0}

	_, err = svc.GetKimiCodeQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrProviderKeyInvalid))
}

// TestGetKimiCodeQuota_DecodeError verifies that a 200 response with a body
// that isn't valid JSON surfaces a decode error.
func TestGetKimiCodeQuota_DecodeError(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("test-api-key", masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-kimi-code",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client()}

	_, err = svc.GetKimiCodeQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}
