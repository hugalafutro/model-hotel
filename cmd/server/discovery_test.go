package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/proxy"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ---------------------------------------------------------------------------
// Integration test database setup
// ---------------------------------------------------------------------------

var cmdTestDB *db.DB
var cmdTestDBURL string

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	var setupErr error
	cmdTestDBURL, setupErr = db.SetupTestDB("cmdserver")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("cmdserver")

	cmdTestDB, err = db.New(ctx, cmdTestDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer cmdTestDB.Close()

	util.CloseDockerClient()
	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testDiscoveryDeps(t *testing.T) discoveryDeps {
	t.Helper()
	pool := cmdTestDB.Pool()
	return discoveryDeps{
		cfg:          &config.Config{MasterKey: "test-master-key-1234567890abcdef"},
		pool:         pool,
		providerRepo: provider.NewRepository(pool),
		modelRepo:    model.NewRepository(pool),
		failoverRepo: failover.NewRepository(pool),
		dialer:       proxy.NewSafeDialer([]string{"127.0.0.1"}, nil),
		settingsRepo: settings.NewRepository(pool),
	}
}

// wipeDiscoveryState clears the tables discovery writes to so tests don't
// bleed into each other (models cascade-delete with their provider).
func wipeDiscoveryState(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM model_failover_groups`,
		`DELETE FROM providers`,
		`DELETE FROM discovery_changes`,
	} {
		if _, err := cmdTestDB.Pool().Exec(ctx, stmt); err != nil {
			t.Fatalf("cleanup %q failed: %v", stmt, err)
		}
	}
}

// closedTestPool returns a pool whose connections are already closed, for
// exercising DB-error branches.
func closedTestPool(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(context.Background(), cmdTestDBURL, 2, 1)
	if err != nil {
		t.Fatalf("failed to open second test DB handle: %v", err)
	}
	database.Close()
	return database
}

// waitForEvent blocks until an event of the wanted type arrives on ch.
func waitForEvent(t *testing.T, ch chan events.Event, wantType string) events.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == wantType {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", wantType)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunDiscoveryNoProviders(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)

	result := runDiscovery(testDiscoveryDeps(t), "test")
	if result.ProvidersScanned != 0 || result.ProvidersFailed != 0 || result.ModelsDiscovered != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
}

// TestRunDiscoveryPrunesChangeJournal pins that a full runDiscovery call, not
// just PruneDiscoveryChanges in isolation, ends by trimming the journal to
// ClaimWindow. TestPruneDiscoveryChanges (internal/api) already covers the
// SQL; this covers the wiring that calls it from the scheduled/startup loop.
func TestRunDiscoveryPrunesChangeJournal(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()
	pool := cmdTestDB.Pool()

	seeds := []struct {
		modelID string
		age     time.Duration
	}{
		{"old-row", api.ClaimWindow + 24*time.Hour}, // seen and past the window: must be pruned
		{"recent-row", time.Hour},                   // seen but recent: must survive
	}
	for _, s := range seeds {
		diff := &api.DiscoveryDiff{Added: []api.ModelChange{{ModelID: s.modelID, Reason: "test"}}}
		if _, err := api.AppendDiscoveryChange(ctx, pool, "test", nil, "", diff); err != nil {
			t.Fatalf("seed %s: %v", s.modelID, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE discovery_changes SET detected_at = now() - $1::interval, seen = true
			  WHERE detected_at = (SELECT MAX(detected_at) FROM discovery_changes)`,
			s.age.String()); err != nil {
			t.Fatalf("age %s: %v", s.modelID, err)
		}
	}

	runDiscovery(testDiscoveryDeps(t), "test")

	rows, err := pool.Query(ctx, `SELECT diff->'added'->0->>'model_id' FROM discovery_changes`)
	if err != nil {
		t.Fatalf("query remaining rows: %v", err)
	}
	defer rows.Close()
	remaining := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		remaining[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if remaining["old-row"] {
		t.Error("old seen row survived runDiscovery's prune")
	}
	if !remaining["recent-row"] {
		t.Error("recent seen row was pruned by runDiscovery; should have survived")
	}
}

// seedOutstandingGroupClaim inserts a failover group discovery disabled the
// given interval ago, which is a COUNTED claim (dead hotel/<model> routing).
// A group rather than a gone model so the run needs no enabled provider: an
// enabled provider would be scanned, fail against its fake base URL, and fill
// result.Errors with noise the isolation assertions below are about.
// revalidateCustomGroups skips already-disabled groups, so the seed survives
// the run untouched.
func seedOutstandingGroupClaim(t *testing.T, disabledFor time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := cmdTestDB.Pool().Exec(ctx,
		`INSERT INTO model_failover_groups (id, display_model, priority_order, group_enabled, auto_disabled_at)
		 VALUES (gen_random_uuid(), 'claim-alert-group', '[]'::jsonb, false, now() - $1::interval)`,
		disabledFor.String()); err != nil {
		t.Fatalf("seed disabled failover group: %v", err)
	}
	// The alert latch is an ordinary settings row, so it outlives the table
	// wipes and would suppress the very event these tests wait for.
	if _, err := cmdTestDB.Pool().Exec(ctx,
		`DELETE FROM settings WHERE key LIKE '_discovery_claim_alert%'`); err != nil {
		t.Fatalf("clear alert latch: %v", err)
	}
}

// TestRunDiscoveryAlertsOnUnaddressedClaims pins the call site: a full
// runDiscovery must end by evaluating the outstanding-claims alert and, when
// the oldest counted claim is past the threshold, publish it and persist the
// edge latch that stops the next scan re-firing. The evaluation logic itself is
// covered in internal/api; this is the wiring, which nothing else observes.
func TestRunDiscoveryAlertsOnUnaddressedClaims(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	seedOutstandingGroupClaim(t, 20*24*time.Hour)

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	runDiscovery(testDiscoveryDeps(t), "test")

	ev := waitForEvent(t, ch, api.EventTypeClaimsOutstanding)
	if got := ev.Metadata["claim_count"]; got != 1 {
		t.Errorf("claim_count = %v, want 1", got)
	}

	var latch string
	if err := cmdTestDB.Pool().QueryRow(context.Background(),
		`SELECT value FROM settings WHERE key = '_discovery_claim_alert_fired_at'`).Scan(&latch); err != nil {
		t.Fatalf("the alert must persist its edge latch: %v", err)
	}
	if latch == "" {
		t.Error("edge latch written empty; the next scan would re-fire the same alert")
	}
}

// TestRunDiscoveryClaimAlertFailureDoesNotFailRun pins that the alert is
// housekeeping, exactly like the journal prune beside it. The settings store is
// backed by a closed pool, so the alert's latch write genuinely fails after the
// event has been published; the run must still report a clean result rather
// than folding a notification problem into the scan's own outcome.
//
// The published-event assertion is what keeps this honest: without it, deleting
// the whole evaluation would satisfy "the run reports no errors" vacuously.
func TestRunDiscoveryClaimAlertFailureDoesNotFailRun(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	seedOutstandingGroupClaim(t, 20*24*time.Hour)

	deps := testDiscoveryDeps(t)
	deps.settingsRepo = settings.NewRepository(closedTestPool(t).Pool())

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	result := runDiscovery(deps, "test")

	waitForEvent(t, ch, api.EventTypeClaimsOutstanding)
	if len(result.Errors) != 0 {
		t.Errorf("a failed alert latch must not become a discovery error, got %v", result.Errors)
	}
	if result.ProvidersScanned != 0 || result.ProvidersFailed != 0 {
		t.Errorf("unexpected scan counters after an alert failure: %+v", result)
	}
}

func TestRunDiscoveryListError(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	deps := testDiscoveryDeps(t)
	broken := closedTestPool(t)
	deps.pool = broken.Pool()
	deps.providerRepo = provider.NewRepository(broken.Pool())

	result := runDiscovery(deps, "test")
	if len(result.Errors) == 0 {
		t.Fatal("expected a list-providers error")
	}
}

// TestRunDiscoveryHappyPath drives a full discovery cycle against a mock
// OpenAI-compatible provider: models are discovered, upserted, and the
// change-feed nudge event fires.
func TestRunDiscoveryHappyPath(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "test-model-a", "object": "model", "owned_by": "tester"},
				{"id": "test-model-b", "object": "model", "owned_by": "tester"},
			},
		})
	}))
	defer srv.Close()

	deps := testDiscoveryDeps(t)
	// Keyless provider (nil key material) pointed at the mock server.
	p, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "cmdserver-discovery-test",
		BaseURL: srv.URL + "/v1",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	// A second provider serving the same model IDs, so the failover sync
	// creates an auto group and records run-wide failover churn.
	if _, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "cmdserver-discovery-test-b",
		BaseURL: srv.URL + "/v1b",
	}, nil, nil, nil); err != nil {
		t.Fatalf("failed to create second provider: %v", err)
	}
	// A disabled provider is skipped entirely (never scanned, never counted).
	disabled, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "cmdserver-disabled-test",
		BaseURL: "http://127.0.0.1:1/v1",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create disabled provider: %v", err)
	}
	if _, err := deps.pool.Exec(ctx, `UPDATE providers SET enabled = false WHERE id = $1`, disabled.ID); err != nil {
		t.Fatalf("failed to disable provider: %v", err)
	}

	// Load a models.dev cache carrying pricing for one of the mock models, so
	// the scan's enrichment step runs for real and its result is observable on
	// the upserted row.
	mdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"testcorp":{"id":"testcorp","name":"TestCorp","api":"","models":{
			"test-model-a":{"id":"test-model-a","name":"Test Model A","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":1.5,"output":6},"limit":{"context":32000,"output":8000}}
		}}}`))
	}))
	defer mdSrv.Close()
	mdBase := mdSrv.Client().Transport
	mdClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = mdSrv.Listener.Addr().String()
		return mdBase.RoundTrip(req)
	})}
	if err := provider.LoadModelsDevWithClient(ctx, mdClient); err != nil {
		t.Fatalf("failed to load models.dev cache: %v", err)
	}
	t.Cleanup(provider.ResetModelsDevCache)

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	result := runDiscovery(deps, "test")

	if result.ProvidersScanned != 2 {
		t.Errorf("expected 2 providers scanned, got %d", result.ProvidersScanned)
	}
	if result.ProvidersFailed != 0 {
		t.Errorf("expected 0 failures, got %d (%v)", result.ProvidersFailed, result.Errors)
	}
	if result.ModelsDiscovered != 4 {
		t.Errorf("expected 4 models discovered, got %d", result.ModelsDiscovered)
	}

	models, err := deps.modelRepo.List(ctx, &p.ID)
	if err != nil {
		t.Fatalf("failed to list models: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models upserted for the first provider, got %d", len(models))
	}
	// models.dev enrichment ran during the scan: the covered model carries the
	// cache's pricing, the uncovered one stays unpriced.
	for _, m := range models {
		switch m.ModelID {
		case "test-model-a":
			if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 1.5 {
				t.Errorf("test-model-a InputPricePerMillion = %v, want 1.5 (models.dev enrichment)", m.InputPricePerMillion)
			}
		case "test-model-b":
			if m.InputPricePerMillion != nil {
				t.Errorf("test-model-b InputPricePerMillion = %v, want nil (not in models.dev)", m.InputPricePerMillion)
			}
		}
	}

	// Both providers expose the same model IDs, so auto failover groups formed.
	var groups int
	if err := deps.pool.QueryRow(ctx,
		`SELECT count(*) FROM model_failover_groups WHERE auto_created = true`).Scan(&groups); err != nil {
		t.Fatalf("failed to count failover groups: %v", err)
	}
	if groups != 2 {
		t.Errorf("expected 2 auto failover groups, got %d", groups)
	}

	// The new models produce a change-feed row, which fires the badge nudge.
	waitForEvent(t, ch, "discovery.changes_pending")

	// last_discovered_at was stamped on the scanned provider only.
	fresh, err := deps.providerRepo.List(ctx)
	if err != nil {
		t.Fatalf("failed to re-list providers: %v", err)
	}
	for _, fp := range fresh {
		switch fp.ID {
		case p.ID:
			if fp.LastDiscoveredAt == nil {
				t.Error("expected last_discovered_at to be stamped after discovery")
			}
		case disabled.ID:
			if fp.LastDiscoveredAt != nil {
				t.Error("disabled provider must not be scanned")
			}
		}
	}
}

func TestScanProviderUnreachable(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()

	deps := testDiscoveryDeps(t)
	p, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "cmdserver-unreachable-test",
		BaseURL: "http://127.0.0.1:1/v1", // nothing listens on port 1
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	svc := provider.NewDiscoveryService(deps.dialer.DialContext, deps.dialer.CheckRedirect)
	var result DiscoveryResult
	changed, ok := scanProvider(ctx, deps, svc, p, "test", &result)
	if changed {
		t.Error("expected no change row for a failed scan")
	}
	if ok {
		t.Error("expected a failed scan to report not-ok, which is what keeps its retired rows out of the prune")
	}
	if result.ProvidersFailed != 1 || len(result.Errors) != 1 {
		t.Errorf("expected a recorded failure, got %+v", result)
	}

	// The failed attempt still stamps last_discovered_at.
	fresh, err := deps.providerRepo.List(ctx)
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}
	if len(fresh) != 1 || fresh[0].LastDiscoveredAt == nil {
		t.Error("expected last_discovered_at to be stamped after a failed scan")
	}
}

func TestTouchLastDiscoveredError(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	broken := closedTestPool(t)
	// Only logs; must not panic on a dead pool.
	touchLastDiscovered(context.Background(), broken.Pool(), &provider.Provider{Name: "x"})
}

func TestMaybeStartupDiscovery(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	deps := testDiscoveryDeps(t)
	settingsRepo := newTestSettingsRepo()
	ctx := context.Background()

	t.Run("disabled_by_setting", func(t *testing.T) {
		wipeDiscoveryState(t)
		if err := settingsRepo.Set(ctx, "discovery_on_startup", "false"); err != nil {
			t.Fatalf("failed to set setting: %v", err)
		}
		defer func() { _ = settingsRepo.Set(ctx, "discovery_on_startup", "true") }()
		// Must return without launching discovery; nothing observable to
		// assert beyond not panicking and not touching providers.
		maybeStartupDiscovery(deps, settingsRepo)
	})

	t.Run("skips_recently_discovered", func(t *testing.T) {
		wipeDiscoveryState(t)
		p, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    "cmdserver-recent-test",
			BaseURL: "http://127.0.0.1:1/v1",
		}, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}
		touchLastDiscovered(ctx, deps.pool, p)
		// Recently-discovered guard fires: no background run is launched, so
		// the unreachable provider is never scanned again.
		maybeStartupDiscovery(deps, settingsRepo)
	})

	t.Run("runs_in_background", func(t *testing.T) {
		wipeDiscoveryState(t)
		ch := events.DefaultBus.Subscribe()
		defer events.DefaultBus.Unsubscribe(ch)

		maybeStartupDiscovery(deps, settingsRepo)

		// Zero providers: the background run completes immediately with a
		// success event.
		ev := waitForEvent(t, ch, "discovery.complete")
		if ev.Severity != "success" {
			t.Errorf("expected success severity, got %q", ev.Severity)
		}
	})
}

// ---------------------------------------------------------------------------
// Retired-row prune
// ---------------------------------------------------------------------------

// newListingServer answers GET /models with the given ids, the minimal
// OpenAI-compatible listing discovery accepts.
func newListingServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		type item struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		}
		items := make([]item, 0, len(ids))
		for _, id := range ids {
			items = append(items, item{ID: id, Object: "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": items})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// createTestProvider creates an enabled keyless provider whose OpenAI-compatible
// base URL is baseURL + "/v1", and returns its id.
func createTestProvider(t *testing.T, deps discoveryDeps, name, baseURL string) uuid.UUID {
	t.Helper()
	p, err := deps.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    name,
		BaseURL: baseURL + "/v1",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider %s: %v", name, err)
	}
	return p.ID
}

// seedRetiredRow inserts a discovery-retired model row last seen `age` ago.
func seedRetiredRow(t *testing.T, providerID uuid.UUID, modelID string, age time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := cmdTestDB.Pool().Exec(context.Background(), `
		INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually, last_seen_at)
		VALUES ($1, $2, $3, $3, false, false, now() - $4::interval)`,
		id, providerID, modelID, age.String()); err != nil {
		t.Fatalf("seed retired row %s: %v", modelID, err)
	}
	return id
}

func modelExists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := cmdTestDB.Pool().QueryRow(context.Background(), `SELECT count(*) FROM models WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("exists: %v", err)
	}
	return n == 1
}

// TestRunDiscoveryPrunesRetiredModels pins the happy path and the horizon: a
// successfully scanned provider's retired rows older than model_prune_days go,
// younger ones stay, and the result reports the count.
func TestRunDiscoveryPrunesRetiredModels(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	deps := testDiscoveryDeps(t)
	ctx := context.Background()
	srv := newListingServer(t, "alive")
	prov := createTestProvider(t, deps, "prune-ok", srv.URL)
	old := seedRetiredRow(t, prov, "dead-old", 40*24*time.Hour)
	young := seedRetiredRow(t, prov, "dead-young", 10*24*time.Hour)
	if err := deps.settingsRepo.Set(ctx, "model_prune_days", "30"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Cleanup(func() { _ = deps.settingsRepo.Set(context.Background(), "model_prune_days", "30") })

	result := runDiscovery(deps, "test")

	if result.ModelsPruned != 1 {
		t.Errorf("ModelsPruned = %d, want 1 (errors: %v)", result.ModelsPruned, result.Errors)
	}
	if modelExists(t, old) {
		t.Error("dead-old survived: retired 40 days ago with a 30 day horizon")
	}
	if !modelExists(t, young) {
		t.Error("dead-young was pruned: retired 10 days ago with a 30 day horizon")
	}
}

// TestRunDiscoveryPruneOffKeepsRows pins the off switch.
func TestRunDiscoveryPruneOffKeepsRows(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	deps := testDiscoveryDeps(t)
	ctx := context.Background()
	srv := newListingServer(t, "alive")
	prov := createTestProvider(t, deps, "prune-off", srv.URL)
	old := seedRetiredRow(t, prov, "dead-old", 40*24*time.Hour)
	if err := deps.settingsRepo.Set(ctx, "model_prune_days", "0"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Cleanup(func() { _ = deps.settingsRepo.Set(context.Background(), "model_prune_days", "30") })

	result := runDiscovery(deps, "test")

	if result.ModelsPruned != 0 || !modelExists(t, old) {
		t.Errorf("prune ran with model_prune_days=0: pruned=%d exists=%v", result.ModelsPruned, modelExists(t, old))
	}
}

// TestRunDiscoveryPruneSkipsFailedProvider pins the scope guard: a provider
// whose scan failed this pass keeps every retired row, however old.
func TestRunDiscoveryPruneSkipsFailedProvider(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	deps := testDiscoveryDeps(t)
	ctx := context.Background()
	okSrv := newListingServer(t, "alive")
	okProv := createTestProvider(t, deps, "prune-scan-ok", okSrv.URL)
	badProv := createTestProvider(t, deps, "prune-scan-bad", "http://127.0.0.1:1") // nothing listens on port 1
	okOld := seedRetiredRow(t, okProv, "ok-dead", 40*24*time.Hour)
	badOld := seedRetiredRow(t, badProv, "bad-dead", 40*24*time.Hour)
	if err := deps.settingsRepo.Set(ctx, "model_prune_days", "30"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Cleanup(func() { _ = deps.settingsRepo.Set(context.Background(), "model_prune_days", "30") })

	result := runDiscovery(deps, "test")

	if result.ProvidersFailed != 1 {
		t.Fatalf("ProvidersFailed = %d, want 1 (the test needs one failing scan)", result.ProvidersFailed)
	}
	if modelExists(t, okOld) {
		t.Error("ok-dead survived although its provider scanned fine")
	}
	if !modelExists(t, badOld) {
		t.Error("bad-dead was pruned although its provider's scan failed this pass")
	}
	if result.ModelsPruned != 1 {
		t.Errorf("ModelsPruned = %d, want 1", result.ModelsPruned)
	}
}

// TestRunDiscoveryPruneSkipsProviderWithUpsertFailure pins the other half of
// the scope guard. A provider that answered its listing but whose rows could
// not all be written has no trustworthy membership picture either, so it is
// out of the prune's scope exactly like an unreachable one. The failure is
// forced with a listed model id carrying a NUL byte, which Postgres rejects
// for a text column: that one upsert errors while the rest of the pass
// carries on.
func TestRunDiscoveryPruneSkipsProviderWithUpsertFailure(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	deps := testDiscoveryDeps(t)
	ctx := context.Background()
	srv := newListingServer(t, "alive", "dead\x00nul")
	prov := createTestProvider(t, deps, "prune-upsert-fail", srv.URL)
	old := seedRetiredRow(t, prov, "dead-old", 40*24*time.Hour)
	if err := deps.settingsRepo.Set(ctx, "model_prune_days", "30"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Cleanup(func() { _ = deps.settingsRepo.Set(context.Background(), "model_prune_days", "30") })

	result := runDiscovery(deps, "test")

	if result.ProvidersFailed != 0 {
		t.Fatalf("ProvidersFailed = %d, want 0 (the listing itself succeeds)", result.ProvidersFailed)
	}
	if result.ModelsPruned != 0 {
		t.Errorf("ModelsPruned = %d, want 0 after an upsert failure", result.ModelsPruned)
	}
	if !modelExists(t, old) {
		t.Error("dead-old was pruned although one of its provider's upserts failed this pass")
	}
}

// TestRunDiscoveryPruneRejectsUnusableHorizon pins the guard on the setting's
// value. Negative and unparseable values have no horizon at all, a value
// under the floor is inert because every retired row still counts as flapping
// inside the claim window, and a value past the API's ceiling overflows the
// duration arithmetic into a future horizon that would match every retired
// row, so all four skip the prune.
func TestRunDiscoveryPruneRejectsUnusableHorizon(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	t.Cleanup(func() {
		_ = settings.NewRepository(cmdTestDB.Pool()).Set(context.Background(), "model_prune_days", "30")
	})

	for _, value := range []string{"-5", "10", "200000", "abc"} {
		t.Run(value, func(t *testing.T) {
			wipeDiscoveryState(t)
			deps := testDiscoveryDeps(t)
			ctx := context.Background()
			srv := newListingServer(t, "alive")
			prov := createTestProvider(t, deps, "prune-horizon-"+value, srv.URL)
			old := seedRetiredRow(t, prov, "dead-old", 40*24*time.Hour)
			if err := deps.settingsRepo.Set(ctx, "model_prune_days", value); err != nil {
				t.Fatalf("set: %v", err)
			}

			result := runDiscovery(deps, "test")

			if result.ModelsPruned != 0 {
				t.Errorf("ModelsPruned = %d, want 0 for model_prune_days=%q", result.ModelsPruned, value)
			}
			if !modelExists(t, old) {
				t.Errorf("dead-old was pruned with model_prune_days=%q", value)
			}
		})
	}
}

// TestRecordMissingModelsUntrustedWhenSuspect pins the verdict the prune's
// scope guard consumes: a scan whose confirmation probe cannot run reports
// trusted=false, so scanProvider withholds that provider from pruning. A
// cancelled context makes the probe's wait fail at once, which is the cheapest
// way to reach the suspect branch without the real probe delays.
func TestRecordMissingModelsUntrustedWhenSuspect(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	deps := testDiscoveryDeps(t)
	ctx := context.Background()
	srv := newListingServer(t, "alive")
	provID := createTestProvider(t, deps, "miss-suspect", srv.URL)
	p, err := deps.providerRepo.Get(ctx, provID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	// An enabled row the (empty) listing below no longer names: one absentee,
	// so the probe loop runs and immediately hits the cancelled context.
	if _, err := deps.pool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled)
		VALUES ($1, $2, 'was-here', 'was-here', true)`, uuid.New(), provID); err != nil {
		t.Fatalf("seed enabled row: %v", err)
	}
	snapshot, err := api.SnapshotProviderModels(ctx, deps.modelRepo, provID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	svc := provider.NewDiscoveryService(deps.dialer.DialContext, deps.dialer.CheckRedirect)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	disabled, trusted := recordMissingModels(cancelled, deps, svc, p, nil, snapshot)

	if trusted {
		t.Error("a suspect scan reported trusted=true")
	}
	if len(disabled) != 0 {
		t.Errorf("a suspect scan disabled %d models, want 0", len(disabled))
	}
	var enabled bool
	if err := deps.pool.QueryRow(ctx, `SELECT enabled FROM models WHERE provider_id = $1 AND model_id = 'was-here'`, provID).Scan(&enabled); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !enabled {
		t.Error("a suspect scan must not record a miss")
	}
}
