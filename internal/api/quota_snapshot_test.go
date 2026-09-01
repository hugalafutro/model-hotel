package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	h.newDiscovery = func() *provider.DiscoveryService {
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

	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(2) }

	h.PollQuotasOnce(context.Background())

	snap, _ := h.quotaRepo.Get(context.Background(), id, "usage")
	if snap == nil || snap.Source != "poll" {
		t.Fatalf("stale fleet snapshot should fall back to self-poll, got %+v", snap)
	}
}

func TestPollQuotasOnce_UpsertsEnabledProviders(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-poll", "https://api.nano-gpt.com/v1", true)

	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }

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

	h.newDiscovery = func() *provider.DiscoveryService {
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
// pins and *which* providers it declared recovered. Reset/ResetAll panic:
// the quota refresh must never clear a circuit.
type pinReleaseRecorder struct {
	mu       sync.Mutex
	calls    []map[uuid.UUID]struct{}
	allCalls int
}

func (p *pinReleaseRecorder) Status() []failover.ProviderStatus       { return nil }
func (p *pinReleaseRecorder) StatusDetail() []failover.ProviderStatus { return nil }

func (p *pinReleaseRecorder) Reset(uuid.UUID) failover.State {
	panic("the quota refresh must never reset a circuit")
}

func (p *pinReleaseRecorder) ResetModel(uuid.UUID, string) (failover.State, bool) {
	return failover.StateClosed, false
}
func (p *pinReleaseRecorder) ResetAll() (int, int) {
	panic("the quota refresh must never reset a circuit")
}

func (p *pinReleaseRecorder) ReleaseQuotaPins(recovered map[uuid.UUID]struct{}) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, recovered)
	return 0
}

// ReleaseAllQuotaPins is counted separately: a refresh must never take the
// blunt release-all path, so a test that sees this move has caught the
// disabled-polling behaviour leaking into an ordinary poll pass.
func (p *pinReleaseRecorder) ReleaseAllQuotaPins() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allCalls++
	return 0
}

// ApplyQuotaPins records nothing: these tests are about the release half of the
// contract, and returning 0 keeps a refresh that retargets pins from disturbing
// the release assertions.
func (p *pinReleaseRecorder) ApplyQuotaPins(map[uuid.UUID]time.Time) int { return 0 }

func (p *pinReleaseRecorder) recorded() []map[uuid.UUID]struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[uuid.UUID]struct{}(nil), p.calls...)
}

func (p *pinReleaseRecorder) recordedAll() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allCalls
}

// TestRefreshQuotaAdvice_ReleasesPinsForRecoveredProviders is the auto-unpin
// path. A pin is stamped on when a circuit opens and nothing used to revisit it,
// so an operator who topped up a spent plan watched a healthy provider stay
// benched for the rest of a pin that can run to 24 hours. A successful refresh
// now hands the breaker exactly one set, once: the providers it assessed fresh
// and found no longer exhausted, which lose their pin and fall back to the
// configured cooldown.
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
	if _, ok := set[recoveredID]; !ok {
		t.Error("a provider assessed fresh and no longer exhausted must be reported as recovered, so its pin is lifted")
	}
	if _, ok := set[spentID]; ok {
		t.Error("a provider whose window is still spent must be absent from the recovered set, keeping its pin")
	}
}

// fixedQuotaAdvisor pins every provider to the same far-off deadline, so a test
// can open several circuits and have each of them carry a quota pin without
// caring which provider is which.
type fixedQuotaAdvisor struct{ at time.Time }

func (f fixedQuotaAdvisor) ResetsAt(uuid.UUID) (time.Time, bool) { return f.at, true }

// TestRefreshQuotaAdvice_ReleasesPinsOnlyOnFreshRecoveryEvidence drives the real
// breaker (not a recorder) through a refresh and asserts on observable breaker
// state, because the rule being guarded is a whole-path one: a provider is
// missing from the advice map for three different reasons and only one of them
// is recovery.
//
// The stale case is the one that matters most. A provider whose quota fetches
// keep failing keeps its last snapshot, that snapshot ages past the staleness
// bound, and the provider then looks exactly like one that recovered. Releasing
// its pin would probe a provider that is still genuinely exhausted — the feature
// throwing itself away precisely when quota fetching is broken.
func TestRefreshQuotaAdvice_ReleasesPinsOnlyOnFreshRecoveryEvidence(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	// A real breaker, with a cooldown short enough that a 6h quota deadline
	// always clears the "pinning must never shorten a wait" floor.
	cb := failover.NewCircuitBreaker(nil)
	cb.Threshold = 1
	cb.Cooldown = time.Minute
	cb.SetQuotaAdvisor(fixedQuotaAdvisor{at: time.Now().Add(6 * time.Hour)})
	h.SetCircuitBreaker(cb)

	now := time.Now()
	resetsAt := now.Add(4 * time.Hour)
	// maxAge is three refresh intervals; the interval defaults to 5 minutes, so
	// anything older than 15 minutes is stale.
	cases := []struct {
		name       string
		payload    json.RawMessage
		fetchedAt  time.Time
		seed       bool
		wantPinned bool
		lastError  string
		why        string
	}{
		{
			name:      "zai-recovered",
			payload:   json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":5000,"nextResetTime":0}]}}`),
			fetchedAt: now, seed: true, wantPinned: false,
			why: "a fresh snapshot assessed as not exhausted is the only affirmative recovery evidence there is",
		},
		{
			name:    "zai-still-spent",
			payload: exhaustedZaiCodingPayload(t, resetsAt), fetchedAt: now, seed: true, wantPinned: true,
			why: "a provider this refresh still assessed as exhausted must keep its pin",
		},
		{
			name:    "zai-stale",
			payload: exhaustedZaiCodingPayload(t, resetsAt), fetchedAt: now.Add(-time.Hour), seed: true, wantPinned: true,
			why: "a snapshot too old to trust is not recovery evidence — the window is probably still spent",
		},
		{
			name:    "zai-unassessable",
			payload: json.RawMessage(`{"data":{"limits":"unexpected shape"}}`), fetchedAt: now, seed: true, wantPinned: true,
			why: "a payload that could not be assessed says nothing about the window",
		},
		{
			name: "zai-no-snapshot", seed: false, wantPinned: true,
			why: "a provider with no snapshot at all has never reported recovery",
		},
		{
			name:      "zai-failed-refresh",
			payload:   json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":5000,"nextResetTime":0}]}}`),
			fetchedAt: now, seed: true, wantPinned: true, lastError: "upstream 500",
			why: "a fresh, healthy-looking snapshot whose latest refresh attempt failed is not affirmative recovery evidence — RecordFailure preserves the last good payload, so this looks identical to a genuine recovery except for LastError",
		},
	}

	ids := make([]uuid.UUID, len(cases))
	for i, c := range cases {
		ids[i] = insertQuotaPollProvider(t, h.dbPool.Pool(), c.name, "https://api.z.ai", true)
		if c.seed {
			if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
				ProviderID: ids[i], Kind: "usage", HTTPStatus: 200, Source: "poll",
				FetchedAt: c.fetchedAt, Payload: c.payload,
			}); err != nil {
				t.Fatalf("seed %s snapshot: %v", c.name, err)
			}
			if c.lastError != "" {
				if err := h.quotaRepo.RecordFailure(ctx, ids[i], "usage", c.lastError); err != nil {
					t.Fatalf("record failure for %s: %v", c.name, err)
				}
			}
		}
		cb.RecordFailure(ids[i], c.name, "", failover.Cause{})
	}

	pinned := pinnedByProvider(cb)
	for i, c := range cases {
		if !pinned[ids[i].String()] {
			t.Fatalf("setup: %s must start out quota-pinned", c.name)
		}
	}

	h.RefreshQuotaAdvice(ctx)

	pinned = pinnedByProvider(cb)
	for i, c := range cases {
		if got := pinned[ids[i].String()]; got != c.wantPinned {
			t.Errorf("%s: got quota_pinned=%v, want %v — %s", c.name, got, c.wantPinned, c.why)
		}
	}

	// Releasing a pin shortens a cooldown; it must never close a circuit.
	if got := cb.GetState(ids[0], ""); got == failover.StateClosed {
		t.Errorf("got state %v for the recovered provider, want it still open — quota must never close a circuit", got)
	}
}

// TestRefreshQuotaAdvice_FleetImportedFailureMarkerRetainsPin is the same
// recovery-evidence rule, one layer deeper: across the fleet boundary. The
// snapshots a member acts on mostly arrive from the primary rather than from
// its own poll, so a member must classify an imported row exactly as the
// primary would. RecordFailure keeps the last good payload and fetched_at and
// sets only last_error, so if that marker is lost anywhere between the
// primary's export and the member's store, the member sees a fresh, healthy
// snapshot, calls it recovery, and releases the quota pin on a provider whose
// window is still spent — permitting an early half-open probe against a
// provider the primary itself is still holding open.
//
// Both cases go in through the real fleet import handler, and both assert
// through real breaker state rather than the advice maps. The marker-free case
// is the control: it must still release, so the test proves the marker is what
// makes the difference rather than some unrelated property of the payload.
func TestRefreshQuotaAdvice_FleetImportedFailureMarkerRetainsPin(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.SetQuotaAdvisor(NewQuotaAdvisor())
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	cb := failover.NewCircuitBreaker(nil)
	cb.Threshold = 1
	cb.Cooldown = time.Minute
	cb.SetQuotaAdvisor(fixedQuotaAdvisor{at: time.Now().Add(6 * time.Hour)})
	h.SetCircuitBreaker(cb)

	// One healthy, not-exhausted z.ai reading, imported by two providers. The
	// only difference between them is the failure marker on the wire.
	const healthy = `{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":5000,"nextResetTime":0}]}}`

	cases := []struct {
		name       string
		lastError  string
		wantPinned bool
		why        string
	}{
		{
			name: "zai-fleet-failed-refresh", lastError: "upstream 500", wantPinned: true,
			why: "an imported snapshot whose latest refresh failed on the primary is not recovery evidence on the member either — the marker has to survive the fleet boundary or the member releases a pin the primary is still holding",
		},
		{
			name: "zai-fleet-healthy", lastError: "", wantPinned: false,
			why: "the identical imported snapshot without a marker is affirmative recovery evidence and must release the pin — this is what proves the marker, not the payload, made the difference above",
		},
	}

	ids := make([]uuid.UUID, len(cases))
	fetchedAt := time.Now().UTC()
	for i, c := range cases {
		ids[i] = insertQuotaPollProvider(t, h.dbPool.Pool(), c.name, "https://api.z.ai", true)

		wire := QuotaSnapshotWire{
			ProviderName: c.name,
			Kind:         "usage",
			Payload:      json.RawMessage(healthy),
			HTTPStatus:   200,
			FetchedAt:    fetchedAt,
			LastError:    c.lastError,
		}
		body, err := json.Marshal(map[string]any{"snapshots": []QuotaSnapshotWire{wire}})
		if err != nil {
			t.Fatalf("marshal %s wire: %v", c.name, err)
		}
		rr := httptest.NewRecorder()
		fleet.ReceiveSnapshots(rr, httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("import %s: want 200, got %d: %s", c.name, rr.Code, rr.Body.String())
		}

		cb.RecordFailure(ids[i], c.name, "", failover.Cause{})
	}

	pinned := pinnedByProvider(cb)
	for i, c := range cases {
		if !pinned[ids[i].String()] {
			t.Fatalf("setup: %s must start out quota-pinned", c.name)
		}
	}

	h.RefreshQuotaAdvice(ctx)

	pinned = pinnedByProvider(cb)
	for i, c := range cases {
		if got := pinned[ids[i].String()]; got != c.wantPinned {
			t.Errorf("%s: got quota_pinned=%v, want %v — %s", c.name, got, c.wantPinned, c.why)
		}
	}
}

// TestDisableQuotaAdvice_ReleasesPinsThatARefreshWouldKeep is the counterpart to
// the recovery-evidence rule, and the case that makes the two rules consistent
// rather than contradictory. While the poller is running, a provider still
// assessed as exhausted keeps its pin. Switch polling off and that same provider
// loses it: no refresh will ever report its recovery again, so the pin would be
// served out to the 24h ceiling on evidence the operator deliberately stopped
// collecting — and the documented meaning of setting the interval to 0 is that
// the feature stops acting on this node.
func TestDisableQuotaAdvice_ReleasesPinsThatARefreshWouldKeep(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	adv := NewQuotaAdvisor()
	h.SetQuotaAdvisor(adv)

	cb := failover.NewCircuitBreaker(nil)
	cb.Threshold = 1
	cb.Cooldown = time.Minute
	cb.SetQuotaAdvisor(fixedQuotaAdvisor{at: time.Now().Add(6 * time.Hour)})
	h.SetCircuitBreaker(cb)

	spentID := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-still-spent", "https://api.z.ai", true)
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: spentID, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
	}); err != nil {
		t.Fatalf("seed exhausted snapshot: %v", err)
	}
	cb.RecordFailure(spentID, "zai-still-spent", "", failover.Cause{})

	// While polling runs, this provider's pin survives every refresh: it is
	// still exhausted, which is affirmative evidence in the other direction.
	h.RefreshQuotaAdvice(ctx)
	if !pinnedByProvider(cb)[spentID.String()] {
		t.Fatal("setup: a still-exhausted provider must keep its pin while polling runs")
	}
	if _, ok := adv.ResetsAt(spentID); !ok {
		t.Fatal("setup: a fresh exhausted snapshot must be advised")
	}

	h.DisableQuotaAdvice(ctx)

	if pinnedByProvider(cb)[spentID.String()] {
		t.Error("disabling polling must release a pin already in force, not leave the provider benched for its ceiling")
	}
	if _, ok := adv.ResetsAt(spentID); ok {
		t.Error("disabling polling must drop the advice too, so nothing new can be pinned")
	}
	// Still only a cooldown change: the circuit stays open and HTTP decides
	// recovery through the ordinary half-open probe.
	if got := cb.GetState(spentID, ""); got != failover.StateOpen {
		t.Errorf("got state %v, want open — disabling quota advice must not close a circuit", got)
	}
	if got := cb.Status()[0].CooldownMs; got != cb.Cooldown.Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the configured cooldown %d back", got, cb.Cooldown.Milliseconds())
	}
}

// TestRefreshQuotaAdvice_NeverTakesTheReleaseAllPath guards the boundary between
// the two releases from the other side: an ordinary refresh, successful or not,
// must never reach for the blunt lever that only a disabled poller may pull.
func TestRefreshQuotaAdvice_NeverTakesTheReleaseAllPath(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	rec := &pinReleaseRecorder{}
	h.SetCircuitBreaker(rec)

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-spent-releaseall", "https://api.z.ai", true)
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		Payload: exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
	}); err != nil {
		t.Fatalf("seed exhausted snapshot: %v", err)
	}

	h.RefreshQuotaAdvice(ctx)

	if got := rec.recordedAll(); got != 0 {
		t.Errorf("got %d ReleaseAllQuotaPins call(s) from a refresh, want 0 — only a disabled poller may release every pin", got)
	}
	if got := len(rec.recorded()); got != 1 {
		t.Errorf("got %d ReleaseQuotaPins call(s), want exactly 1 per successful refresh", got)
	}
}

func pinnedByProvider(cb *failover.CircuitBreaker) map[string]bool {
	out := make(map[string]bool)
	for _, s := range cb.Status() {
		out[s.ProviderID] = s.QuotaPinned
	}
	return out
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

// ---------------------------------------------------------------------------
// Breaker-open quota nudge
// ---------------------------------------------------------------------------

// waitForQuotaSnapshot returns the snapshot a nudge stores from its own
// goroutine, failing once the deadline passes. The poll interval is a retry
// cadence, not a synchronization point: the assertion is the snapshot existing.
func waitForQuotaSnapshot(t *testing.T, h *Handler, id uuid.UUID, kind string) *quota.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, err := h.quotaRepo.Get(context.Background(), id, kind)
		if err != nil {
			t.Fatalf("get snapshot: %v", err)
		}
		if snap != nil {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the nudge to store a snapshot")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// exhaustedZaiCodingDiscovery returns a discovery service whose zai-coding quota
// endpoint reports a fully spent 5-hour window resetting at resetsAt. percentage
// is set because the stored payload is a re-marshal of ZAICodingQuotaResponse,
// whose non-pointer percentage field always serializes: the normalizer trusts a
// percentage inside [0,100] over remaining, so a payload carrying only
// remaining=0 would round-trip as percentage=0 and read as healthy.
func exhaustedZaiCodingDiscovery(resetsAt time.Time) *provider.DiscoveryService {
	body := `{"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":0,"percentage":100,"nextResetTime":` +
		strconv.FormatInt(resetsAt.UnixMilli(), 10) + `}]}}`
	ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
		Transport: &mockTransport{roundTripFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}},
	})
	ds.SetRetryBaseDelay(time.Millisecond)
	return ds
}

// TestNudgeQuotaPoll_DebouncesRepeatOpens verifies a flapping circuit cannot
// turn into a poll storm against the provider it just gave up on. The discovery
// service is built on the caller's goroutine before the poll is spawned, so the
// factory count is a synchronous readout of how many nudges were admitted.
func TestNudgeQuotaPoll_DebouncesRepeatOpens(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-nudge-debounce", "https://api.nano-gpt.com/v1", true)

	var admitted atomic.Int64
	h.newDiscovery = func() *provider.DiscoveryService {
		admitted.Add(1)
		return nanoGPTPollDiscovery(7)
	}

	h.NudgeQuotaPoll(id)
	waitForQuotaSnapshot(t, h, id, "usage")
	if got := admitted.Load(); got != 1 {
		t.Fatalf("first nudge: got %d polls, want 1", got)
	}

	h.NudgeQuotaPoll(id)
	if got := admitted.Load(); got != 1 {
		t.Fatalf("a second open inside the debounce window must not poll again, got %d polls", got)
	}
}

// TestNudgeQuotaPoll_SkipsProvidersWithNothingToPoll verifies the two cases
// where an open circuit says nothing about quota: the provider is switched off,
// or its type exposes no quota endpoint at all.
func TestNudgeQuotaPoll_SkipsProvidersWithNothingToPoll(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		enabled bool
		why     string
	}{
		{"disabled", "https://api.nano-gpt.com/v1", false, "a disabled provider serves no traffic and must not be called"},
		{"no-quota-endpoint", "https://api.openai.com/v1", true, "this provider type exposes no quota endpoint"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHandler(t)
			id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nudge-"+c.name, c.baseURL, c.enabled)

			var admitted atomic.Int64
			h.newDiscovery = func() *provider.DiscoveryService {
				admitted.Add(1)
				return nanoGPTPollDiscovery(1)
			}

			h.NudgeQuotaPoll(id)

			if got := admitted.Load(); got != 0 {
				t.Fatalf("got %d polls, want 0: %s", got, c.why)
			}
		})
	}
}

// TestNudgeQuotaPoll_RetargetsAdviceFromFreshReading is the whole point of the
// nudge: a circuit that opens on a spent quota window gets its cooldown pinned
// from advice, and advice only exists once a snapshot has been read and
// assessed. Polling alone is not enough, so this asserts the advisor carries the
// provider's real reset deadline afterwards rather than only that a row landed.
func TestNudgeQuotaPoll_RetargetsAdviceFromFreshReading(t *testing.T) {
	h := newTestHandler(t)
	advisor := NewQuotaAdvisor()
	h.SetQuotaAdvisor(advisor)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-nudge", "https://api.z.ai/api/coding/paas/v4", true)

	resetsAt := time.Now().Add(4 * time.Hour)
	h.newDiscovery = func() *provider.DiscoveryService { return exhaustedZaiCodingDiscovery(resetsAt) }

	h.NudgeQuotaPoll(id)

	snap := waitForQuotaSnapshot(t, h, id, "usage")
	if snap.Source != "poll" {
		t.Fatalf("want source=poll, got %q", snap.Source)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if at, ok := advisor.ResetsAt(id); ok {
			if at.UnixMilli() != resetsAt.UnixMilli() {
				t.Fatalf("advised reset %s, want %s", at, resetsAt)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the nudge to refresh quota advice")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNudgeQuotaPoll_RetargetsAnAlreadyOpenCircuit wires the feature end to end.
// A circuit opens while nothing yet knows the provider's window is spent, so it
// is pinned to nothing and holds an ordinary cooldown. The open transition
// triggers the nudge, and the reading it fetches must move the retry instant on
// that same circuit out to the real reset, rather than waiting for the circuit
// to fail a probe and open a second time.
func TestNudgeQuotaPoll_RetargetsAnAlreadyOpenCircuit(t *testing.T) {
	h := newTestHandler(t)

	// One advisor behind both sides, as main.go wires it.
	advisor := NewQuotaAdvisor()
	h.SetQuotaAdvisor(advisor)
	cb := failover.NewCircuitBreaker(nil) // threshold 5, cooldown 60s
	cb.SetQuotaAdvisor(advisor)
	cb.SetOnOpen(h.NudgeQuotaPoll)
	h.SetCircuitBreaker(cb)

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-repin", "https://api.z.ai/api/coding/paas/v4", true)

	resetsAt := time.Now().Add(4 * time.Hour)
	h.newDiscovery = func() *provider.DiscoveryService { return exhaustedZaiCodingDiscovery(resetsAt) }

	// The advisor is empty until the nudge refreshes it, so this open is
	// necessarily unpinned and lands on the 60s default cooldown.
	for i := 0; i < 5; i++ {
		cb.RecordFailure(id, "zai-repin", "", failover.Cause{})
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		statuses := cb.Status()
		if len(statuses) == 1 && statuses[0].QuotaPinned {
			got := statuses[0].CooldownMs
			minMs := (4*time.Hour - time.Minute).Milliseconds()
			maxMs := (4 * time.Hour).Milliseconds() * 21 / 20
			if got < minMs || got > maxMs {
				t.Fatalf("got CooldownMs=%d, want the circuit retargeted to the ~4h quota reset (within [%d,%d])", got, minMs, maxMs)
			}
			if statuses[0].State != "open" {
				t.Fatalf("got state %q, want the circuit still open: quota must never close a circuit", statuses[0].State)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the nudge to retarget the open circuit, last status %+v", statuses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNudgeQuotaPoll_SpentPollBudgetDoesNotWipeAdvice pins the blast radius of a
// hanging quota endpoint. The poll and the advice refresh must not share one
// budget: a fetch that runs to its deadline leaves the refresh unable to read,
// and a refresh that cannot read fails closed by clearing *every* provider's
// advice, not just this one's. That would turn one slow endpoint into a
// fleet-wide loss of pins until the next successful background pass, under
// exactly the conditions the nudge exists for, since a provider that just failed
// requests is a provider whose quota endpoint is plausibly hanging too.
//
// A cancelled context is what an exhausted poll budget leaves behind at the
// moment the refresh would start, without the test waiting out the real budget.
func TestNudgeQuotaPoll_SpentPollBudgetDoesNotWipeAdvice(t *testing.T) {
	h := newTestHandler(t)
	advisor := NewQuotaAdvisor()
	h.SetQuotaAdvisor(advisor)
	ctx := context.Background()

	// Two exhausted providers, so the assertion can tell a wipe confined to the
	// nudged provider apart from the fleet-wide one this guards against.
	nudged := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-nudged", "https://api.z.ai/api/coding/paas/v4", true)
	bystander := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-bystander", "https://gw.z.ai/api/coding/paas/v4", true)

	resetsAt := time.Now().Add(4 * time.Hour)
	for _, id := range []uuid.UUID{nudged, bystander} {
		if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
			ProviderID: id, Kind: "usage", Payload: exhaustedZaiCodingPayload(t, resetsAt),
			HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
		}); err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
	}

	h.RefreshQuotaAdvice(ctx)
	for _, c := range []struct {
		name string
		id   uuid.UUID
	}{{"nudged", nudged}, {"bystander", bystander}} {
		if _, ok := advisor.ResetsAt(c.id); !ok {
			t.Fatalf("setup: %s provider must start out advised", c.name)
		}
	}

	prov, err := h.providerRepo.Get(ctx, nudged)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}

	spent, cancel := context.WithCancel(ctx)
	cancel()

	h.runQuotaNudge(spent, exhaustedZaiCodingDiscovery(resetsAt), prov, "usage")

	if _, ok := advisor.ResetsAt(bystander); !ok {
		t.Error("a spent poll budget wiped advice for an unrelated provider: the refresh must not run on a context the poll could exhaust")
	}
	if _, ok := advisor.ResetsAt(nudged); !ok {
		t.Error("a spent poll budget dropped the nudged provider's own advice, which its stored snapshot still supports")
	}
}
