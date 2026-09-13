package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

// zenListing serves a Zen /models listing carrying the given ids and records
// the Authorization header it saw.
func zenListing(t *testing.T, ids ...string) (*httptest.Server, *string) {
	t.Helper()
	var seenAuth string
	body := `{"data": [`
	for i, id := range ids {
		if i > 0 {
			body += ","
		}
		body += `{"id": "` + id + `", "object": "model", "owned_by": "opencode", "created": 1234567890}`
	}
	body += `]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seenAuth
}

// zenModelsDev seeds the models.dev cache with Zen's opencode entry: one free
// model, one paid, one listed without a cost.
func zenModelsDev(t *testing.T) {
	t.Helper()
	specs := map[string]*ModelsDevModelSpec{
		"big-pickle":      {ID: "big-pickle", Cost: &ModelsDevCost{Input: 0, Output: 0}, Limit: ModelsDevLimit{Context: 200000, Output: 32000}},
		"claude-sonnet-5": {ID: "claude-sonnet-5", Cost: &ModelsDevCost{Input: 2, Output: 10}},
		"costless":        {ID: "costless"},
		"mimo-v2.5-free":  {ID: "mimo-v2.5-free", Cost: &ModelsDevCost{Input: 0, Output: 0}},
	}
	// The cross-provider index lists an unrelated model at zero under the same
	// bare id as a paid Zen model; only the canonical opencode entry may decide.
	elsewhere := map[string]*ModelsDevModelSpec{
		"claude-sonnet-5": {ID: "claude-sonnet-5", Cost: &ModelsDevCost{Input: 0, Output: 0}},
	}
	setupCacheWithModels(t, elsewhere)
	modelsDevCache.mu.Lock()
	modelsDevCache.byProvider = map[string]map[string]*ModelsDevModelSpec{"opencode": specs}
	modelsDevCache.mu.Unlock()
}

// A keyed provider surfaces every model the listing carries, as bare stubs
// for models.dev to enrich, and nothing the listing does not carry: there is
// no catalog left to resurrect a model Zen has dropped. A model models.dev
// prices at zero is written as a known free one, so enrichment (which reads a
// zero as no figure) does not report it unpriced on every scan.
func TestDiscoverOpenCodeZen_KeyedListsEveryLiveModel(t *testing.T) {
	zenModelsDev(t)
	const apiKey = "test-api-key"
	server, seenAuth := zenListing(t, "big-pickle", "claude-sonnet-5", "mimo-v2.5-free", "unknown-custom-model")

	kp, err := auth.Encrypt(apiKey, "test-master-key-1234567890123456")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL, EncryptedKey: kp.Ciphertext, KeyNonce: kp.Nonce, KeySalt: kp.Salt}

	models, err := (&DiscoveryService{httpClient: server.Client()}).discoverOpenCodeZen(context.Background(), provider, apiKey)
	assert.NoError(t, err)
	assert.Equal(t, "Bearer "+apiKey, *seenAuth)
	assert.ElementsMatch(t, []string{"big-pickle", "claude-sonnet-5", "mimo-v2.5-free", "unknown-custom-model"}, modelIDsOf(models))
	for _, m := range models {
		assert.Equal(t, "opencode", m.OwnedBy, m.ModelID)
		assert.Contains(t, m.Capabilities, `"streaming":true`, m.ModelID)
		switch m.ModelID {
		case "big-pickle", "mimo-v2.5-free":
			assert.Equal(t, 0.0, *m.InputPricePerMillion, "%s: a free model is priced at a known zero", m.ModelID)
			assert.Equal(t, 0.0, *m.OutputPricePerMillion, "%s: a free model is priced at a known zero", m.ModelID)
		default:
			// claude-sonnet-5 is zero somewhere on the cross-provider index
			// and paid on Zen's own entry; only the latter counts.
			assert.Nil(t, m.InputPricePerMillion, "%s: the price is models.dev's to fill", m.ModelID)
			assert.Nil(t, m.OutputPricePerMillion, "%s: the price is models.dev's to fill", m.ModelID)
		}
		if m.ModelID == "mimo-v2.5-free" {
			// The catalog row restricts the input modalities models.dev
			// over-advertises; backfill applies it before enrichment can fill
			// the stub's empty list.
			assert.Equal(t, `["text","image","video"]`, m.InputModalities)
			assert.Equal(t, "MiMo V2.5 (Free)", m.DisplayName)
			continue
		}
		assert.Equal(t, m.ModelID, m.DisplayName, m.ModelID)
	}
}

// A keyless provider keeps only the live models models.dev prices at zero: a
// paid model is skipped, and so is one models.dev lists without a cost or does
// not list at all, since nothing says either is free.
func TestDiscoverOpenCodeZen_KeylessKeepsTheFreeModels(t *testing.T) {
	zenModelsDev(t)
	server, _ := zenListing(t, "big-pickle", "claude-sonnet-5", "costless", "unknown-custom-model")
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := (&DiscoveryService{httpClient: server.Client()}).discoverOpenCodeZen(context.Background(), provider, "")
	assert.NoError(t, err)
	assert.Equal(t, []string{"big-pickle"}, modelIDsOf(models))
	assert.Equal(t, 0.0, *models[0].InputPricePerMillion)
	assert.Equal(t, 0.0, *models[0].OutputPricePerMillion)
}

// Without the models.dev cache a keyless provider cannot tell a free model
// from a paid one, so it surfaces nothing rather than a model a keyless caller
// cannot use.
func TestDiscoverOpenCodeZen_KeylessWithoutModelsDevKeepsNothing(t *testing.T) {
	t.Cleanup(ResetModelsDevCache)
	ResetModelsDevCache()
	server, _ := zenListing(t, "big-pickle")
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := (&DiscoveryService{httpClient: server.Client()}).discoverOpenCodeZen(context.Background(), provider, "")
	assert.NoError(t, err)
	assert.Empty(t, models)
}
