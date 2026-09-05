package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// lockedReadDB returns a *db.DB with a single connection and a short
// statement_timeout, plus a lock function that takes ACCESS EXCLUSIVE on table
// through a holder transaction on the shared test pool.
//
// Recipe for reaching a rows.Err() branch deterministically: run the query once
// through the returned pool (pgx prepares the statement on its one connection),
// lock the table, run it again. The prepared statement's execute phase blocks
// on the lock, statement_timeout cancels it, and pgx reports the failure from
// rows.Err() after Query itself returned rows. Without the warm-up the prepare
// would block instead and fail at Query.
func lockedReadDB(t *testing.T, table string) (pool *db.DB, lock func() (unlock func())) {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "250")
	u.RawQuery = q.Encode()

	pool, err = db.New(context.Background(), u.String(), 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(pool.Close)

	lock = func() func() {
		tx, err := apiTestDB.Pool().Begin(context.Background())
		if err != nil {
			t.Fatalf("begin holder tx: %v", err)
		}
		if _, err := tx.Exec(context.Background(), "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatalf("lock %s: %v", table, err)
		}
		unlock := func() { _ = tx.Rollback(context.Background()) }
		t.Cleanup(unlock)
		return unlock
	}
	return pool, lock
}

var errReadBack = errors.New("read-back failed")

func adminGet(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	return req
}

// expectReadFailure runs the request twice through handler: once to warm the
// prepared statement (must succeed), then again with the table locked (must be
// a 500 carrying wantMsg).
func expectReadFailure(t *testing.T, handler http.HandlerFunc, warm, locked *http.Request, lock func() func(), wantMsg string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, warm)
	if rec.Code != http.StatusOK {
		t.Fatalf("warm-up: status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	unlock := lock()
	defer unlock()
	rec = httptest.NewRecorder()
	handler(rec, locked)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("locked: status = %d, body = %s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), wantMsg) {
		t.Errorf("locked: body = %q, want %q", rec.Body.String(), wantMsg)
	}
}

func TestGetTimeSeries_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "request_logs")
	h := NewStatsHandler(pool.Pool(), &mockAdminAuth{validateFn: func(string) bool { return true }})
	expectReadFailure(t, h.GetTimeSeries, adminGet("/stats/timeseries?period=24h"), adminGet("/stats/timeseries?period=24h"), lock, "failed to read time series")
}

func TestGetProviderDistribution_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "request_logs")
	h := NewStatsHandler(pool.Pool(), &mockAdminAuth{validateFn: func(string) bool { return true }})
	expectReadFailure(t, h.GetProviderDistribution, adminGet("/stats/providers?period=24h"), adminGet("/stats/providers?period=24h"), lock, "failed to read provider distribution")
}

// TestStatsQueries_ReadFailurePropagates covers the two per-query readers that
// GetStats never reaches with a locked table (an earlier query fails first):
// the virtual-key ranking returns the read error, the best-effort latency
// breakdown logs it and leaves the slice empty.
func TestStatsQueries_ReadFailurePropagates(t *testing.T) {
	pool, lock := lockedReadDB(t, "request_logs")
	h := NewStatsHandler(pool.Pool(), nil)
	ctx := context.Background()
	since := time.Now().Add(-24 * time.Hour)

	warm := &StatsResponse{ByVirtualKey: map[string]int64{}}
	if err := h.statByVirtualKey(ctx, warm, "requests", since, true, ""); err != nil {
		t.Fatalf("warm-up statByVirtualKey: %v", err)
	}
	h.statLatencyBreakdown(ctx, warm, "", "", nil, since)

	unlock := lock()
	defer unlock()
	stats := &StatsResponse{ByVirtualKey: map[string]int64{}}
	if err := h.statByVirtualKey(ctx, stats, "requests", since, true, ""); err == nil {
		t.Error("statByVirtualKey: want read error under lock, got nil")
	}
	h.statLatencyBreakdown(ctx, stats, "", "", nil, since)
	if len(stats.ByProviderLatency) != 0 {
		t.Errorf("statLatencyBreakdown under lock: got %d entries, want none", len(stats.ByProviderLatency))
	}
}

func TestListLogs_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "request_logs")
	h := &Handler{dbPool: pool, adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }}}
	// Different pages so the second call misses the response cache and reaches
	// the database; the SQL text is identical, so the statement is already prepared.
	expectReadFailure(t, h.ListLogs, adminGet("/logs?page=1&per_page=5"), adminGet("/logs?page=2&per_page=5"), lock, "failed to read logs")
}

func TestListLogsCursor_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "request_logs")
	h := &Handler{dbPool: pool, adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }}}
	expectReadFailure(t, h.ListLogsCursor, adminGet("/logs/cursor?limit=5"), adminGet("/logs/cursor?limit=5"), lock, "failed to read logs")
}

func TestGetAppLogsHistory_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "app_logs")
	t.Cleanup(invalidateAppLogCountCache)
	h := &Handler{dbPool: pool, adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }}}
	expectReadFailure(t, h.getAppLogsHistory, adminGet("/logs/app?history=true"), adminGet("/logs/app?history=true"), lock, "failed to read app logs")
}

func TestGetAppLogsCursor_ReadFailureIs500(t *testing.T) {
	pool, lock := lockedReadDB(t, "app_logs")
	t.Cleanup(invalidateAppLogCountCache)
	h := &Handler{dbPool: pool, adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }}}
	expectReadFailure(t, h.GetAppLogsCursor, adminGet("/logs/app/cursor?limit=5"), adminGet("/logs/app/cursor?limit=5"), lock, "failed to read app logs")
}

// TestGetAppLogCounts_FailureKeepsCachedCounts covers the stale-cache path: a
// count query that fails after the TTL expired returns the previous counts
// instead of publishing zeros.
func TestGetAppLogCounts_FailureKeepsCachedCounts(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	t.Cleanup(invalidateAppLogCountCache)
	ctx := context.Background()
	if _, err := apiTestDB.Pool().Exec(ctx, `INSERT INTO app_logs (timestamp, level, source, message) VALUES (now(), 'error', 'stale-test', 'seed')`); err != nil {
		t.Fatalf("seed app log: %v", err)
	}
	t.Cleanup(func() { _, _ = apiTestDB.Pool().Exec(ctx, `DELETE FROM app_logs WHERE source = 'stale-test'`) })

	invalidateAppLogCountCache()
	live := &Handler{dbPool: apiTestDB}
	primed, _ := live.getAppLogCounts(ctx)
	if primed["error"] < 1 {
		t.Fatalf("priming counts: error = %d, want >= 1", primed["error"])
	}

	// Expire the cache, then fail the refresh with a closed pool.
	appLogCountCache.Lock()
	appLogCountCache.fetchedAt = time.Time{}
	appLogCountCache.Unlock()
	dead, err := db.New(ctx, apiTestDBURL, 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	dead.Close()
	got, _ := (&Handler{dbPool: dead}).getAppLogCounts(ctx)
	if got["error"] != primed["error"] {
		t.Errorf("after failed refresh: error = %d, want the cached %d", got["error"], primed["error"])
	}
}

// TestListProviders_ReadFailureIs500 covers both result sets: model counts
// (models) and token totals (request_logs).
func TestListProviders_ReadFailureIs500(t *testing.T) {
	for _, tc := range []struct{ table, msg string }{
		{"models", "failed to read model counts"},
		{"request_logs", "failed to read token counts"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			pool, lock := lockedReadDB(t, tc.table)
			h := &Handler{
				dbPool:       pool,
				adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
				providerRepo: &mockProviderStore{listFn: func(context.Context) ([]*provider.Provider, error) { return nil, nil }},
			}
			expectReadFailure(t, h.ListProviders, adminGet("/providers"), adminGet("/providers"), lock, tc.msg)
		})
	}
}

// TestSettingsWrite_PostCommitReadFailureIs500 covers the update and reset
// handlers when the write commits but the read-back fails: the client gets a
// 500, never a 200 with an empty settings map.
func TestSettingsWrite_PostCommitReadFailureIs500(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	sets := &mockSettingsStore{
		getAllFn:       func(context.Context) (map[string]string, error) { return nil, errReadBack },
		setTxFn:        func(context.Context, pgx.Tx, string, string) error { return nil },
		deleteKeysTxFn: func(context.Context, pgx.Tx, []string) error { return nil },
	}
	h := testHandler(nil, nil, sets, &mockAdminAuth{validateFn: func(string) bool { return true }}, apiTestDB)

	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"rate_limit_rps": "10"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to read settings") {
		t.Errorf("update: status = %d, body = %q, want 500 read-back failure", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/settings", strings.NewReader(`{"keys": ["rate_limit_rps"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ResetSettings(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to read settings") {
		t.Errorf("reset: status = %d, body = %q, want 500 read-back failure", rec.Code, rec.Body.String())
	}
}
