package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
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

// exhaustedZaiCodingPayload builds a zai-coding usage payload with a spent
// 5-hour window and the given reset deadline, matching the shape Assess
// expects (TOKENS_LIMIT, unit=3, remaining=0).
func exhaustedZaiCodingPayload(t *testing.T, resetsAt time.Time) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": resetsAt.UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal exhausted zai-coding payload: %v", err)
	}
	return b
}

// TestRefreshQuotaAdvice_ThreeIntervalWindowBoundary pins the "three refresh
// intervals" rule end to end (real quotaRepo + providerRepo, not the pure
// buildQuotaAdvice helper): with quota_refresh_interval_min left at its
// default of 5, maxAge resolves to 15 minutes. A 14-minute-old snapshot must
// survive and a 16-minute-old one must not, so a regression that changed the
// "3 *" multiplier to anything else (2x = 10min, or 30x = 150min) would fail
// this specific test even though it might not touch buildQuotaAdvice's own
// unit tests (which take maxAge as a given argument, not derived from the
// setting).
func TestRefreshQuotaAdvice_ThreeIntervalWindowBoundary(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	freshID := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-fresh", "https://api.z.ai", true)
	oldID := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-old", "https://api.z.ai", true)

	resetsAt := time.Now().Add(4 * time.Hour)
	payload := exhaustedZaiCodingPayload(t, resetsAt)

	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: freshID, Kind: "usage", Payload: payload, HTTPStatus: 200, Source: "poll",
		FetchedAt: time.Now().Add(-14 * time.Minute),
	}); err != nil {
		t.Fatalf("seed fresh snapshot: %v", err)
	}
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: oldID, Kind: "usage", Payload: payload, HTTPStatus: 200, Source: "poll",
		FetchedAt: time.Now().Add(-16 * time.Minute),
	}); err != nil {
		t.Fatalf("seed old snapshot: %v", err)
	}

	adv := NewQuotaAdvisor()
	h.SetQuotaAdvisor(adv)

	// quota_refresh_interval_min is left unset so GetInt's default of 5
	// applies, resolving maxAge to 3*5=15 minutes.
	h.RefreshQuotaAdvice(ctx)

	if _, ok := adv.ResetsAt(freshID); !ok {
		t.Error("a 14-minute-old snapshot (within the 15-minute window) must be advised")
	}
	if _, ok := adv.ResetsAt(oldID); ok {
		t.Error("a 16-minute-old snapshot (past the 15-minute window) must not be advised")
	}
}

// attrCaptureHandler records every slog record emitted while installed, with
// its level and attributes, so a log assertion can name the exact line it means.
type attrCaptureHandler struct {
	mu      sync.Mutex
	records []struct {
		level slog.Level
		msg   string
		attrs map[string]any
	}
}

func (h *attrCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *attrCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, struct {
		level slog.Level
		msg   string
		attrs map[string]any
	}{r.Level, r.Message, attrs})
	return nil
}

func (h *attrCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *attrCaptureHandler) WithGroup(string) slog.Handler      { return h }

// last returns the most recent record with the given message.
func (h *attrCaptureHandler) last(msg string) (slog.Level, map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].msg == msg {
			return h.records[i].level, h.records[i].attrs, true
		}
	}
	return 0, nil, false
}

// captureLogs installs a capturing slog handler for the duration of the test.
// debuglog.SetHandler swaps the process-wide default, so it is restored after.
func captureLogs(t *testing.T) *attrCaptureHandler {
	t.Helper()
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	capt := &attrCaptureHandler{}
	debuglog.SetHandler(capt)
	return capt
}

// TestRefreshQuotaAdvice_LogsAdvisedProvidersAtInfoOnlyWhenAdvising covers the
// operator's log trail for a pinned circuit. The advice-refresh line was emitted
// at Debug, which is off in normal production, so nothing outside the Failover
// page and the SSE stream said a provider was being advised as quota-exhausted —
// for a state that can last a day. It is promoted to Info, but only when there
// is something to report: a line emitted on every poll pass regardless would
// just be noise, and noise gets filtered back out.
func TestRefreshQuotaAdvice_LogsAdvisedProvidersAtInfoOnlyWhenAdvising(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-advice-log", "https://api.z.ai", true)

	capt := captureLogs(t)

	// A healthy provider: nothing advised, so the line stays at Debug.
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":5000,"nextResetTime":0}]}}`),
	}); err != nil {
		t.Fatalf("seed healthy snapshot: %v", err)
	}
	h.RefreshQuotaAdvice(ctx)

	level, attrs, found := capt.last("quota: advice refreshed")
	if !found {
		t.Fatal("the advice refresh must log a line in both directions")
	}
	if level != slog.LevelDebug {
		t.Errorf("got level %v with nothing advised, want debug — an every-pass Info line is noise", level)
	}
	if got, _ := attrs["advised_providers"].(int64); got != 0 {
		t.Errorf("got advised_providers=%v, want 0", attrs["advised_providers"])
	}

	// The window is now spent: an operator needs to see this in a normal log.
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
	}); err != nil {
		t.Fatalf("seed exhausted snapshot: %v", err)
	}
	h.RefreshQuotaAdvice(ctx)

	level, attrs, found = capt.last("quota: advice refreshed")
	if !found {
		t.Fatal("the advice refresh must log a line in both directions")
	}
	if level != slog.LevelInfo {
		t.Errorf("got level %v with a provider advised, want info — debug is off in production", level)
	}
	if got, _ := attrs["advised_providers"].(int64); got != 1 {
		t.Errorf("got advised_providers=%v, want 1", attrs["advised_providers"])
	}
}

// pinReleaseRecorder is a CircuitBreakerControl that records every
// ReleaseQuotaPins call, so a test can assert both *that* the refresh released
// pins and *which* providers it declared still exhausted. Reset/ResetAll panic:
// the quota refresh must never clear a circuit.
type pinReleaseRecorder struct {
	mu    sync.Mutex
	calls []map[uuid.UUID]struct{}
}

func (p *pinReleaseRecorder) Status() []failover.ProviderStatus { return nil }

func (p *pinReleaseRecorder) Reset(uuid.UUID) failover.State {
	panic("the quota refresh must never reset a circuit")
}

func (p *pinReleaseRecorder) ResetAll() (int, int) {
	panic("the quota refresh must never reset a circuit")
}

func (p *pinReleaseRecorder) ReleaseQuotaPins(stillExhausted map[uuid.UUID]struct{}) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, stillExhausted)
	return 0
}

func (p *pinReleaseRecorder) recorded() []map[uuid.UUID]struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[uuid.UUID]struct{}(nil), p.calls...)
}

// TestRefreshQuotaAdvice_ReleasesPinsForRecoveredProviders is the auto-unpin
// path. A pin is stamped on when a circuit opens and nothing used to revisit it,
// so an operator who topped up a spent plan watched a healthy provider stay
// benched for the rest of a pin that can run to 24 hours. A successful refresh
// now hands the breaker its fresh exhausted set: providers outside it lose their
// pin and fall back to the configured cooldown.
func TestRefreshQuotaAdvice_ReleasesPinsForRecoveredProviders(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	rec := &pinReleaseRecorder{}
	h.SetCircuitBreaker(rec)

	spentID := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-still-spent", "https://api.z.ai", true)
	recoveredID := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-recovered", "https://api.z.ai", true)

	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: spentID, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
	}); err != nil {
		t.Fatalf("seed exhausted snapshot: %v", err)
	}
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: recoveredID, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":5000,"nextResetTime":0}]}}`),
	}); err != nil {
		t.Fatalf("seed recovered snapshot: %v", err)
	}

	h.RefreshQuotaAdvice(ctx)

	calls := rec.recorded()
	if len(calls) != 1 {
		t.Fatalf("got %d ReleaseQuotaPins call(s), want exactly 1 per successful refresh", len(calls))
	}
	set := calls[0]
	if _, ok := set[spentID]; !ok {
		t.Error("a provider whose window is still spent must be reported as still exhausted, keeping its pin")
	}
	if _, ok := set[recoveredID]; ok {
		t.Error("a provider that has recovered must be absent from the exhausted set, so its pin is lifted")
	}
}

// TestRefreshQuotaAdvice_FailedRefreshLeavesPinsUntouched is the safety half of
// the same feature. Both failure paths clear the advice map so stale data cannot
// pin anything new, but neither says a word about provider health — a database
// blip must not drag every quota-pinned provider back into rotation. So a failed
// refresh must not release pins at all.
func TestRefreshQuotaAdvice_FailedRefreshLeavesPinsUntouched(t *testing.T) {
	t.Run("quota repo list failure", func(t *testing.T) {
		h := newTestHandler(t)
		brokenPool, err := pgxpool.New(context.Background(), apiTestDBURL)
		if err != nil {
			t.Fatalf("failed to open pool: %v", err)
		}
		brokenPool.Close()
		h.quotaRepo = quota.NewRepository(brokenPool)
		h.SetQuotaAdvisor(NewQuotaAdvisor())

		rec := &pinReleaseRecorder{}
		h.SetCircuitBreaker(rec)

		h.RefreshQuotaAdvice(context.Background())

		if got := len(rec.recorded()); got != 0 {
			t.Errorf("got %d ReleaseQuotaPins call(s) after a snapshot-list failure, want 0", got)
		}
	})

	t.Run("provider repo list failure", func(t *testing.T) {
		h := newTestHandler(t)
		h.providerRepo = &mockProviderStore{
			listFn: func(context.Context) ([]*provider.Provider, error) {
				return nil, errors.New("provider list boom")
			},
		}
		h.SetQuotaAdvisor(NewQuotaAdvisor())

		rec := &pinReleaseRecorder{}
		h.SetCircuitBreaker(rec)

		h.RefreshQuotaAdvice(context.Background())

		if got := len(rec.recorded()); got != 0 {
			t.Errorf("got %d ReleaseQuotaPins call(s) after a provider-list failure, want 0", got)
		}
	})
}

// TestRefreshQuotaAdvice_QuotaRepoListErrorClearsAdvice verifies the fail-closed
// path: if the snapshot table can't be listed, the advisor must be cleared
// rather than left holding a possibly-stale pin (see the identical reasoning
// on PollQuotasOnce's provider-list failure). Only quotaRepo.List is broken
// here (a closed pool of its own) so providerRepo.List still succeeds,
// isolating this path from the provider-list failure covered below.
func TestRefreshQuotaAdvice_QuotaRepoListErrorClearsAdvice(t *testing.T) {
	h := newTestHandler(t)

	brokenPool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	brokenPool.Close()
	h.quotaRepo = quota.NewRepository(brokenPool)

	adv := NewQuotaAdvisor()
	seedID := uuid.New()
	seedAt := time.Now().Add(time.Hour).Truncate(time.Second)
	adv.Replace(map[uuid.UUID]time.Time{seedID: seedAt})
	h.SetQuotaAdvisor(adv)

	h.RefreshQuotaAdvice(context.Background())

	if _, ok := adv.ResetsAt(seedID); ok {
		t.Fatal("a quotaRepo.List failure must clear the advisor, not leave a stale pin")
	}
}

// TestRefreshQuotaAdvice_ProviderRepoListErrorClearsAdvice covers the second
// fail-closed path: quotaRepo.List succeeds (a real, working repo) while only
// providerRepo.List (needed to build the type map) fails. The advisor must be
// cleared here too rather than left holding whatever it last had.
func TestRefreshQuotaAdvice_ProviderRepoListErrorClearsAdvice(t *testing.T) {
	h := newTestHandler(t)
	h.providerRepo = &mockProviderStore{
		listFn: func(context.Context) ([]*provider.Provider, error) {
			return nil, errors.New("provider list boom")
		},
	}

	adv := NewQuotaAdvisor()
	seedID := uuid.New()
	seedAt := time.Now().Add(time.Hour).Truncate(time.Second)
	adv.Replace(map[uuid.UUID]time.Time{seedID: seedAt})
	h.SetQuotaAdvisor(adv)

	h.RefreshQuotaAdvice(context.Background())

	if _, ok := adv.ResetsAt(seedID); ok {
		t.Fatal("a providerRepo.List failure must clear the advisor, not leave a stale pin")
	}
}

// TestPollQuotasOnce_ProviderListFailureClearsQuotaAdvice verifies the fix for
// the "frozen deadline" gap: when PollQuotasOnce can't even list providers, it
// must clear the advisor (fail closed to no-pin) rather than silently keep
// whatever advice was computed on a previous, now-untrustworthy, pass.
func TestPollQuotasOnce_ProviderListFailureClearsQuotaAdvice(t *testing.T) {
	h := newTestHandler(t)
	h.providerRepo = &mockProviderStore{
		listFn: func(context.Context) ([]*provider.Provider, error) {
			return nil, errors.New("provider list boom")
		},
	}

	adv := NewQuotaAdvisor()
	staleID := uuid.New()
	adv.Replace(map[uuid.UUID]time.Time{staleID: time.Now().Add(time.Hour)})
	h.SetQuotaAdvisor(adv)

	h.PollQuotasOnce(context.Background())

	if _, ok := adv.ResetsAt(staleID); ok {
		t.Error("a providerRepo.List failure in PollQuotasOnce must clear quota advice, not freeze it")
	}
}

// TestClearQuotaAdvice_NilAdvisorNoop verifies ClearQuotaAdvice is safe to
// call on a Handler where SetQuotaAdvisor was never wired (matches the doc
// comment's "no-op" contract).
func TestClearQuotaAdvice_NilAdvisorNoop(t *testing.T) {
	h := newTestHandler(t)
	h.ClearQuotaAdvice(context.Background())
}
