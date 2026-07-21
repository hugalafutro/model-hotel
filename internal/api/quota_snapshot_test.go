package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

func TestQuotaKindFor(t *testing.T) {
	cases := map[string]string{
		"nanogpt": "usage", "zai-coding": "usage", "kimi-code": "usage",
		"minimax": "usage", "openrouter": "usage", "neuralwatt": "usage",
		"deepseek": "balance", "ollama-cloud": "account",
	}
	for pt, want := range cases {
		got, ok := quotaKindFor(pt)
		if !ok || got != want {
			t.Fatalf("%s: got (%q,%v) want (%q,true)", pt, got, ok, want)
		}
	}
	if _, ok := quotaKindFor("openai"); ok {
		t.Fatal("openai should be unsupported")
	}
}

// TestFetchQuotaSnapshot_NeuralWattNilIs204 verifies the free-tier path:
// GetNeuralWattQuota returns (nil, nil) on a 404 from the quota endpoint, and
// fetchQuotaSnapshot must translate that to http_status=204 with a null payload.
func TestFetchQuotaSnapshot_NeuralWattNilIs204(t *testing.T) {
	disc := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
		Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
			// NeuralWatt returns 404 for free-tier keys (no quota endpoint).
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
				Header:     make(http.Header),
			}, nil
		}},
	})
	disc.SetRetryBaseDelay(time.Millisecond)

	prov := createTestProvider(t, "neuralwatt-nil", "https://api.neuralwatt.com", testMasterKeyForDiscovery)

	kind, payload, status, err := fetchQuotaSnapshot(context.Background(), disc, prov, testMasterKeyForDiscovery)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("want status 204, got %d", status)
	}
	if kind != "usage" {
		t.Fatalf("want kind=usage, got %q", kind)
	}
	if string(payload) != "null" {
		t.Fatalf("want payload=null, got %q", string(payload))
	}
}

// TestFetchQuotaSnapshot_Success verifies a normal usage fetch marshals the
// upstream body and reports http_status=200.
func TestFetchQuotaSnapshot_Success(t *testing.T) {
	disc := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
		Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/usage") {
				resp := `{"active":true,"provider":"nanogpt","providerStatus":"active","providerStatusRaw":"active","limits":{},"dailyInputTokens":{"used":100,"limit":1000},"weeklyInputTokens":{"used":500,"limit":5000},"state":"active"}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(resp)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		}},
	})
	disc.SetRetryBaseDelay(time.Millisecond)

	prov := createTestProvider(t, "nanogpt-ok", "https://api.nano-gpt.com/v1", testMasterKeyForDiscovery)

	kind, payload, status, err := fetchQuotaSnapshot(context.Background(), disc, prov, testMasterKeyForDiscovery)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("want status 200, got %d", status)
	}
	if kind != "usage" {
		t.Fatalf("want kind=usage, got %q", kind)
	}
	if len(payload) == 0 || !strings.Contains(string(payload), "nanogpt") {
		t.Fatalf("want payload containing marshalled upstream body, got %q", string(payload))
	}
}

// TestFetchQuotaSnapshot_UnsupportedType verifies a provider whose type exposes
// no quota endpoint returns an error and zero status.
func TestFetchQuotaSnapshot_UnsupportedType(t *testing.T) {
	disc := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
		Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, http.ErrUseLastResponse
		}},
	})

	prov := createTestProvider(t, "openai-x", "https://api.openai.com/v1", testMasterKeyForDiscovery)

	kind, payload, status, err := fetchQuotaSnapshot(context.Background(), disc, prov, testMasterKeyForDiscovery)
	if err == nil {
		t.Fatal("want error for unsupported provider type")
	}
	if status != 0 || payload != nil || kind != "" {
		t.Fatalf("want zero-value results, got kind=%q payload=%q status=%d", kind, string(payload), status)
	}
}

// insertQuotaPollProvider inserts a provider row (with encrypted key material so
// the fetch layer can decrypt it) directly into the test DB and returns its ID.
func insertQuotaPollProvider(t *testing.T, pool *pgxpool.Pool, name, baseURL string, enabled bool) uuid.UUID {
	t.Helper()
	ek, kn, ks := encryptTestKey(t, "test-api-key", testMasterKey)
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())`,
		id, name, baseURL, ek, kn, ks, enabled)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

// nanoGPTPollDiscovery returns a discovery service whose /usage endpoint reports
// a fresh dailyInputTokens.used value, and 404s everything else.
func nanoGPTPollDiscovery(used int64) *provider.DiscoveryService {
	ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
		Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/usage") {
				body := `{"active":true,"provider":"nanogpt","dailyInputTokens":{"used":` +
					strconv.FormatInt(used, 10) + `,"limit":100}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}},
	})
	ds.SetRetryBaseDelay(time.Millisecond)
	return ds
}

// TestPollQuotasOnce_UpsertsEnabledProviders verifies the poll pass fetches an
// enabled quota-capable provider and stores a fresh source="poll" snapshot.
// TestPollQuotasOnce_SuppressesWhenRecentFleetSnapshot verifies a member fed by
// Front Desk does not also hit upstream while a recent fleet snapshot exists.
func TestPollQuotasOnce_SuppressesWhenRecentFleetSnapshot(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-fleet-recent", "https://api.nano-gpt.com/v1", true)

	// A recent fleet-distributed snapshot already exists.
	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: id, Kind: "usage", Payload: json.RawMessage(`{"used":1}`), HTTPStatus: 200, Source: "fleet", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed fleet snapshot: %v", err)
	}

	called := false
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(_ *http.Request) (*http.Response, error) {
				called = true
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			}},
		})
	}

	h.PollQuotasOnce(context.Background())

	if called {
		t.Fatal("poll must not hit upstream while a recent fleet snapshot exists")
	}
	snap, _ := h.quotaRepo.Get(context.Background(), id, "usage")
	if snap == nil || snap.Source != "fleet" {
		t.Fatalf("the fleet snapshot should remain untouched, got %+v", snap)
	}
}

// TestPollQuotasOnce_PollsWhenFleetSnapshotStale verifies a stale fleet snapshot
// does not suppress the self-poll, so quota is never worse than standalone.
func TestPollQuotasOnce_PollsWhenFleetSnapshotStale(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-fleet-stale", "https://api.nano-gpt.com/v1", true)

	// A stale fleet snapshot (older than the poll interval) must not suppress.
	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: id, Kind: "usage", Payload: json.RawMessage(`{"used":1}`), HTTPStatus: 200, Source: "fleet", FetchedAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed stale fleet snapshot: %v", err)
	}

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(2) }

	h.PollQuotasOnce(context.Background())

	snap, _ := h.quotaRepo.Get(context.Background(), id, "usage")
	if snap == nil || snap.Source != "poll" {
		t.Fatalf("stale fleet snapshot should fall back to self-poll, got %+v", snap)
	}
}

func TestPollQuotasOnce_UpsertsEnabledProviders(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-poll", "https://api.nano-gpt.com/v1", true)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }

	h.PollQuotasOnce(context.Background())

	snap, err := h.quotaRepo.Get(context.Background(), id, "usage")
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("poll should upsert a snapshot, got nil")
	}
	if snap.Source != "poll" {
		t.Fatalf("want source=poll, got %q", snap.Source)
	}
	if snap.HTTPStatus != http.StatusOK {
		t.Fatalf("want http_status=200, got %d", snap.HTTPStatus)
	}
	// JSONB canonicalizes whitespace, so decode semantically instead of a
	// byte/substring compare on the raw payload.
	var got struct {
		DailyInputTokens struct {
			Used int64 `json:"used"`
		} `json:"dailyInputTokens"`
	}
	if uerr := json.Unmarshal(snap.Payload, &got); uerr != nil {
		t.Fatalf("decode payload: %v (%s)", uerr, string(snap.Payload))
	}
	if got.DailyInputTokens.Used != 9 {
		t.Fatalf("want fresh used=9, got %d (%s)", got.DailyInputTokens.Used, string(snap.Payload))
	}
}

// TestPollQuotasOnce_SkipsDisabled verifies a disabled provider is never polled,
// so no snapshot row is created for it.
func TestPollQuotasOnce_SkipsDisabled(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-disabled", "https://api.nano-gpt.com/v1", false)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
				t.Fatalf("disabled provider should not trigger an upstream call to %s", req.URL.String())
				return nil, nil
			}},
		})
	}

	h.PollQuotasOnce(context.Background())

	snap, err := h.quotaRepo.Get(context.Background(), id, "usage")
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snap != nil {
		t.Fatalf("disabled provider should not be polled, got %+v", snap)
	}
}
