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

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/proxy"
	"github.com/hugalafutro/model-hotel/internal/util"
)

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
		t.Skip("test DB unavailable")
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

func TestRunDiscoveryListError(t *testing.T) {
	if cmdTestDB == nil {
		t.Skip("test DB unavailable")
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
		t.Skip("test DB unavailable")
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
		t.Skip("test DB unavailable")
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
	changed := scanProvider(ctx, deps, svc, p, "test", &result)
	if changed {
		t.Error("expected no change row for a failed scan")
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
		t.Skip("test DB unavailable")
	}
	broken := closedTestPool(t)
	// Only logs; must not panic on a dead pool.
	touchLastDiscovered(context.Background(), broken.Pool(), &provider.Provider{Name: "x"})
}

func TestMaybeStartupDiscovery(t *testing.T) {
	if cmdTestDB == nil {
		t.Skip("test DB unavailable")
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
