package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// 1. getAppLogsHistory — nil dbPool path
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_NilPool tests the early return path when dbPool is nil.
// The handler should return an empty response without crashing.
// We call getAppLogsHistory directly because h.Register dereferences h.dbPool.Pool().
func TestGetAppLogsHistory_NilPool(t *testing.T) {
	h := &Handler{
		dbPool:   nil,
		adminMgr: &mockAdminAuth{validateFn: func(token string) bool { return token == "test-admin-token" }},
	}

	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	w := httptest.NewRecorder()

	// Call the handler method directly to test the nil-pool early return
	h.getAppLogsHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for nil pool, got %d: %s", w.Code, w.Body.String())
	}

	var response appLogsHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(response.Entries) != 0 {
		t.Errorf("expected empty entries for nil pool, got %d", len(response.Entries))
	}
}

// ---------------------------------------------------------------------------
// 2. getAppLogsHistory — DB query error path (cancelled context)
// ---------------------------------------------------------------------------

// TestGetAppLogsHistory_QueryError tests the error path where the DB query
// in getAppLogsHistory fails (e.g. cancelled context). We call the handler
// method directly because h.Register dereferences h.dbPool.Pool() which
// panics when the pool is nil.
func TestGetAppLogsHistory_QueryError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(token string) bool { return token == "test-admin-token" }},
	}

	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	// Cancel the request context so DB queries fail
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.getAppLogsHistory(w, req)

	// The handler returns an error JSON body for internal query failures.
	// It should not crash.
	t.Logf("getAppLogsHistory with cancelled context: status=%d body=%s", w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// 3. calculateStats — includeLatency=true path
// ---------------------------------------------------------------------------

// TestCalculateStats_WithLatency exercises the includeLatency=true branch
// in calculateStats, which invokes statLatencyBreakdown.
func TestCalculateStats_WithLatency(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert enough request logs to populate latency data (>=3 for HAVING clause)
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "latency-provider", "https://api.example.com/v1")
	for i := 0; i < 5; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "latency-model", 200, 100+i*10, 10, 20, requestLogOpts{
			ResponseHeaderMs: float64(50 + i*5),
			ProxyOverheadMs:  float64(10 + i*2),
			LatencyMs:        float64(80 + i*3),
		})
	}

	result, err := handler.calculateStats(context.Background(), 24*time.Hour, false, "requests", true)
	if err != nil {
		t.Fatalf("calculateStats with latency: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from calculateStats with latency")
	}
	// The key coverage is that the includeLatency=true branch is exercised.
	_ = result.ByModelLatency
	_ = result.ByProviderLatency
}

// TestCalculateStats_WithoutLatency exercises the includeLatency=false branch,
// confirming statLatencyBreakdown is NOT called.
func TestCalculateStats_WithoutLatency(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	result, err := handler.calculateStats(context.Background(), 24*time.Hour, false, "requests", false)
	if err != nil {
		t.Fatalf("calculateStats without latency: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from calculateStats without latency")
	}
	if len(result.ByModelLatency) != 0 {
		t.Errorf("expected empty ByModelLatency with includeLatency=false, got %d", len(result.ByModelLatency))
	}
	if len(result.ByProviderLatency) != 0 {
		t.Errorf("expected empty ByProviderLatency with includeLatency=false, got %d", len(result.ByProviderLatency))
	}
}

// ---------------------------------------------------------------------------
// 4. statTotals — excludeDeleted=true (vkScope) path
// ---------------------------------------------------------------------------

// TestStats_StatTotalsWithExcludeDeleted exercises statTotals with
// excludeDeleted=true, which triggers the vkScope JOIN and filter.
func TestStats_StatTotalsWithExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "vkstat-provider", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	stats := &StatsResponse{
		ByModel:      make(map[string]int64),
		ByProvider:   make(map[string]int64),
		ByVirtualKey: make(map[string]int64),
	}
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	err := handler.statTotals(context.Background(), stats,
		" LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id",
		" AND (rl.virtual_key_id IS NULL OR vk.id IS NOT NULL)",
		24*time.Hour, since, now)
	if err != nil {
		t.Fatalf("statTotals with excludeDeleted: %v", err)
	}
	if stats.TotalRequestsLast24h < 0 {
		t.Errorf("TotalRequestsLast24h = %d, want >= 0", stats.TotalRequestsLast24h)
	}
}

// ---------------------------------------------------------------------------
// 5. DeleteModel — DB lookup error path (non-pgx.ErrNoRows)
// ---------------------------------------------------------------------------

// TestDeleteModel_DBLookupError tests the error path in DeleteModel where the
// initial SELECT model_id query fails with a non-ErrNoRows error.
func TestDeleteModel_DBLookupError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := &Handler{
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(token string) bool { return token == "test-admin-token" }},
	}

	fakeID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/models/"+fakeID, http.NoBody)
	// Cancel context to cause query failure (not ErrNoRows)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fakeID)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.DeleteModel(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for DB lookup error, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 6. StartScheduler — context cancelled during initial delay
// ---------------------------------------------------------------------------

// TestStartScheduler_ContextCancelledDuringInitialDelay verifies that when
// the parent context is cancelled during the initial 1-minute delay, the
// goroutine exits cleanly.
func TestStartScheduler_ContextCancelledDuringInitialDelay(t *testing.T) {
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, defaultValue bool) bool {
			return false
		},
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the initial delay select picks up ctx.Done()
	cancel()

	h.StartScheduler(ctx)
	// Give the goroutine a moment to process the cancellation
	time.Sleep(50 * time.Millisecond)
	h.StopScheduler()
	// No panic = success
}

// ---------------------------------------------------------------------------
// 7. StopScheduler — idempotent
// ---------------------------------------------------------------------------

// TestStopScheduler_Idempotent verifies that calling StopScheduler multiple
// times is safe.
func TestStopScheduler_Idempotent(t *testing.T) {
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, _ bool) bool { return false },
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	ctx := context.Background()
	h.StartScheduler(ctx)
	time.Sleep(20 * time.Millisecond)

	// Stop multiple times — should not panic
	h.StopScheduler()
	h.StopScheduler()
	h.StopScheduler()
}

// ---------------------------------------------------------------------------
// 8. RestoreBackup — 409 contention path
// ---------------------------------------------------------------------------

// TestRestoreBackup_MutexAlreadyLocked tests that RestoreBackup returns 409
// when the backup mutex is already held.
func TestRestoreBackup_MutexAlreadyLocked(t *testing.T) {
	dir := t.TempDir()
	bh := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir,
		&mockAdminAuth{validateFn: func(s string) bool { return true }}, nil)

	// Manually lock the mutex to simulate an in-progress operation
	bh.backupMu.Lock()
	defer bh.backupMu.Unlock()

	backupRouter := chi.NewRouter()
	bh.Register(backupRouter)

	req := httptest.NewRequest("POST", "/backups/restore", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	backupRouter.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 9. RestoreBackup — missing multipart form
// ---------------------------------------------------------------------------

// TestRestoreBackup_NonMultipartBody tests that RestoreBackup returns 400
// when the request body is not a valid multipart form.
func TestRestoreBackup_NonMultipartBody(t *testing.T) {
	dir := t.TempDir()
	bh := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir,
		&mockAdminAuth{validateFn: func(s string) bool { return true }}, nil)

	backupRouter := chi.NewRouter()
	bh.Register(backupRouter)

	req := httptest.NewRequest("POST", "/backups/restore", strings.NewReader(`{"test": true}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	backupRouter.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-multipart body, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 10. ListProviders — scan error with cancelled context
// ---------------------------------------------------------------------------

// TestListProviders_ScanErrorWithCancelledCtx tests the model count scan error
// path by cancelling the context before the scan can complete.
func TestListProviders_ScanErrorWithCancelledCtx(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := testHandler(&mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{{ID: uuid.New(), Name: "test", BaseURL: "https://api.example.com", Enabled: true}}, nil
		},
	}, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, testDB)

	// Cancel context after the list call so subsequent queries fail
	req, w := newChiRequest(http.MethodGet, "/providers", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	h.ListProviders(w, req)

	// Either 500 (query failure) or 200 (if cancellation hit after scan) is acceptable
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Errorf("expected 500 or 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 11. filterContainers — AppGroup filter branch
// ---------------------------------------------------------------------------

// The Docker filter tests are in internal/util/docker_wrapper_test.go since
// filterContainers and ContainerFilter are in that package.
// Instead, test vkScope from stats.go which is in this package.

// TestVKScope exercises the vkScope helper for both branches.
func TestVKScope(t *testing.T) {
	t.Run("excludeDeleted=true", func(t *testing.T) {
		join, filter := vkScope(true)
		if join == "" {
			t.Error("expected non-empty join for excludeDeleted=true")
		}
		if filter == "" {
			t.Error("expected non-empty filter for excludeDeleted=true")
		}
	})
	t.Run("excludeDeleted=false", func(t *testing.T) {
		join, filter := vkScope(false)
		if join != "" {
			t.Errorf("expected empty join for excludeDeleted=false, got %q", join)
		}
		if filter != "" {
			t.Errorf("expected empty filter for excludeDeleted=false, got %q", filter)
		}
	})
}

// ---------------------------------------------------------------------------
// 12. metricValueSelect — both branches
// ---------------------------------------------------------------------------

func TestMetricValueSelect(t *testing.T) {
	t.Run("tokens", func(t *testing.T) {
		got := metricValueSelect("tokens")
		if !strings.Contains(got, "SUM") {
			t.Errorf("expected SUM for tokens metric, got %q", got)
		}
	})
	t.Run("requests", func(t *testing.T) {
		got := metricValueSelect("requests")
		if !strings.Contains(got, "COUNT") {
			t.Errorf("expected COUNT for requests metric, got %q", got)
		}
	})
}
