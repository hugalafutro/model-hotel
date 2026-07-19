package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// ---------------------------------------------------------------------------
// DetectProviderType
// ---------------------------------------------------------------------------

func TestDetectProviderType_MiniMax(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"intl api host", "https://api.minimax.io/v1", "minimax"},
		{"subdomain gateway", "https://gateway.minimax.io/v1", "minimax"},
		{"bare host", "https://minimax.io", "minimax"},
		{"cn twin stays generic", "https://api.minimaxi.com/v1", "openai"},
		{"lookalike domain stays generic", "https://minimax.io.evil.com", "openai"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != tc.want {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// discoverMiniMax
// ---------------------------------------------------------------------------

// minimaxListing is a trimmed copy of the real /v1/models response
// (live-captured 2026-07-19).
const minimaxListing = `{
	"object": "list",
	"data": [
		{"id": "MiniMax-M3", "object": "model", "created": 1780272000, "owned_by": "minimax"},
		{"id": "MiniMax-M2.5-highspeed", "object": "model", "created": 1780272000, "owned_by": "minimax"}
	]
}`

func TestDiscoverMiniMax_MapsListingMetadata(t *testing.T) {
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
		w.Write([]byte(minimaxListing))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverMiniMax(context.Background(), provider, apiKey)
	assert.NoError(t, err)
	assert.Len(t, models, 2)

	byID := map[string]*model.Model{}
	for _, m := range models {
		byID[m.ModelID] = m
	}

	m3 := byID["MiniMax-M3"]
	if assert.NotNil(t, m3) {
		assert.Equal(t, "minimax", m3.OwnedBy)
		assert.True(t, m3.Enabled)
	}

	hs := byID["MiniMax-M2.5-highspeed"]
	assert.NotNil(t, hs)
}

func TestDiscoverMiniMax_EmptyListing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverMiniMax(context.Background(), provider, "k")
	assert.NoError(t, err)
	assert.Empty(t, models)
}

func TestDiscoverMiniMax_FetchErrorAborts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverMiniMax(context.Background(), provider, "k")
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestDiscoverMiniMax_DecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}
	svc := &DiscoveryService{httpClient: server.Client()}

	models, err := svc.discoverMiniMax(context.Background(), provider, "k")
	assert.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "failed to decode response")
}

// ---------------------------------------------------------------------------
// GetMiniMaxQuota
// ---------------------------------------------------------------------------

// minimaxQuotaPayload is the real /v1/token_plan/remains success response
// (live-captured 2026-07-19, user's fresh Token Plan, both key types).
const minimaxQuotaPayload = `{"model_remains":[{"start_time":1784473200000,"end_time":1784491200000,"remains_time":16420081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"general","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":1,"current_interval_remaining_percent":100,"current_weekly_status":1,"current_weekly_remaining_percent":100},{"start_time":1784419200000,"end_time":1784505600000,"remains_time":30820081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"video","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":3,"current_interval_remaining_percent":100,"current_weekly_status":3,"current_weekly_remaining_percent":100}],"base_resp":{"status_code":0,"status_msg":"success"}}`

// minimaxQuotaNoSubscriptionPayload is the /v1/token_plan/remains response
// for a key with no active Token Plan subscription (live-captured
// 2026-07-19): HTTP 200 with a business error in base_resp.
const minimaxQuotaNoSubscriptionPayload = `{"model_remains":null,"base_resp":{"status_code":2062,"status_msg":"no active token plan subscription"}}`

func TestGetMiniMaxQuota_DecodesTokenPlan(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"
	kp, err := auth.Encrypt(apiKey, masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token_plan/remains" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(minimaxQuotaPayload))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-minimax",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client()}

	quota, err := svc.GetMiniMaxQuota(context.Background(), provider, masterKey)
	assert.NoError(t, err)
	if assert.NotNil(t, quota) {
		assert.Equal(t, 0, quota.BaseResp.StatusCode)
		if assert.Len(t, quota.ModelRemains, 2) {
			general := quota.ModelRemains[0]
			assert.Equal(t, "general", general.ModelName)
			assert.Equal(t, 1, general.CurrentIntervalStatus)
			assert.Equal(t, float64(100), general.CurrentIntervalRemainingPercent)
			assert.Equal(t, int64(1783900800000), general.WeeklyStartTime)
			assert.Equal(t, int64(1784505600000), general.WeeklyEndTime)
		}
	}
}

func TestGetMiniMaxQuota_NoActiveSubscription(t *testing.T) {
	t.Parallel()

	masterKey := "test-master-key-1234567890123456"
	kp, err := auth.Encrypt("test-api-key", masterKey)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(minimaxQuotaNoSubscriptionPayload))
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-minimax",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client()}

	quota, err := svc.GetMiniMaxQuota(context.Background(), provider, masterKey)
	assert.NoError(t, err)
	if assert.NotNil(t, quota) {
		assert.Nil(t, quota.ModelRemains)
		assert.Equal(t, 2062, quota.BaseResp.StatusCode)
	}
}

func TestGetMiniMaxQuota_KeyInvalid(t *testing.T) {
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
		Name:         "test-minimax",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: 0}

	_, err = svc.GetMiniMaxQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderKeyInvalid))
}

func TestGetMiniMaxQuota_UnexpectedStatus(t *testing.T) {
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
		Name:         "test-minimax",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: 0}

	_, err = svc.GetMiniMaxQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrProviderKeyInvalid))
}

func TestGetMiniMaxQuota_DecodeError(t *testing.T) {
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
		Name:         "test-minimax",
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}
	svc := &DiscoveryService{httpClient: server.Client()}

	_, err = svc.GetMiniMaxQuota(context.Background(), provider, masterKey)
	assert.Error(t, err)
}

// TestGetMiniMaxQuota_DecryptFailure verifies that a provider row with a key
// that doesn't decrypt (e.g. corrupted ciphertext/nonce/salt) fails fast with
// a decrypt error and never issues an HTTP request.
func TestGetMiniMaxQuota_DecryptFailure(t *testing.T) {
	t.Parallel()

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-minimax",
		BaseURL:      "https://api.minimax.io/v1",
		EncryptedKey: []byte("not-real-ciphertext"),
		KeyNonce:     []byte("bad-nonce"),
		KeySalt:      []byte("bad-salt"),
	}
	svc := &DiscoveryService{httpClient: http.DefaultClient}

	_, err := svc.GetMiniMaxQuota(context.Background(), provider, "test-master-key-1234567890123456")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt API key")
}

// TestDiscoverModels_MiniMaxDispatch exercises the minimax arm of the
// DiscoverModels provider-type switch end-to-end offline: DetectProviderType
// routes the api.minimax.io host to "minimax", and a mock transport returns a
// canned listing so discoverMiniMax runs without a real network call.
func TestDiscoverModels_MiniMaxDispatch(t *testing.T) {
	t.Parallel()

	const listing = `{
		"object": "list",
		"data": [
			{"id": "MiniMax-M3", "object": "model", "owned_by": "minimax"},
			{"id": "MiniMax-M2.5-highspeed", "object": "model", "owned_by": "minimax"}
		]
	}`

	svc := &DiscoveryService{httpClient: &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				if !strings.Contains(req.URL.Host, "api.minimax.io") {
					return nil, errors.New("unexpected host " + req.URL.Host)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(listing)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-minimax-dispatch",
		BaseURL: "https://api.minimax.io/v1",
	}

	models, err := svc.DiscoverModels(context.Background(), provider, "unused-master-key")
	assert.NoError(t, err)
	assert.Len(t, models, 2)
}
