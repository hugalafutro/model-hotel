package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// kimiListing is a trimmed copy of the real /coding/v1/models response
// (live-captured 2026-07-19).
const kimiListing = `{
	"object": "list",
	"data": [
		{"id": "k3", "object": "model", "created": 1761264000, "display_name": "K3", "type": "model", "context_length": 262144, "supports_reasoning": true, "supports_image_in": true, "supports_video_in": true, "supports_thinking_type": "only"},
		{"id": "kimi-for-coding", "object": "model", "created": 1761264000, "display_name": "K2.7 Coding", "type": "model", "context_length": 262144, "supports_reasoning": true, "supports_image_in": true, "supports_video_in": true, "supports_thinking_type": "only"},
		{"id": "text-only-model", "object": "model", "created": 1761264000, "context_length": 8192, "supports_reasoning": false, "supports_image_in": false, "supports_video_in": false}
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
	assert.Len(t, models, 3)

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
