package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// logScopeFixture is the harness state for the owner-scope tests: two
// log-granted users, one owned virtual key each, and six request_logs rows
// covering both attribution shapes — two keyed rows for alice and one for bob
// (owner resolved through the key), one keyless dashboard-chat row for each of
// them (owner stamped on the row, migration 067), and one keyless row with no
// owner at all, which stays admin-only.
type logScopeFixture struct {
	router                       chi.Router
	aliceToken, bobToken         string
	aliceID, bobID               string
	aliceLogID, bobLogID         string
	aliceChatLogID, bobChatLogID string
}

func setupLogScopeTest(t *testing.T) logScopeFixture {
	t.Helper()
	router, loginAs, mkUser := setupOwnershipTest(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs`); err != nil {
		t.Fatalf("truncate request_logs: %v", err)
	}
	// The offset-list response cache is process-global with a 2s TTL; clear it
	// so a page cached by an earlier test cannot bleed into the assertions.
	globalLogsCache.clear()

	fx := logScopeFixture{router: router}
	fx.aliceID = mkUser("log-alice", []string{string(user.GrantLogs), string(user.GrantUsage)})
	fx.bobID = mkUser("log-bob", []string{string(user.GrantLogs), string(user.GrantUsage)})
	fx.aliceToken = loginAs(fx.aliceID)
	fx.bobToken = loginAs(fx.bobID)

	mkKey := func(name, owner string) string {
		w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
			fmt.Sprintf(`{"name":%q,"owner_user_id":%q}`, name, owner))
		if w.Code != http.StatusCreated {
			t.Fatalf("create key %s: %d %s", name, w.Code, w.Body.String())
		}
		return decodeVK(t, w.Body.Bytes()).ID
	}
	aliceKey := mkKey("alice-key", fx.aliceID)
	bobKey := mkKey("bob-key", fx.bobID)

	// Mirrors what insertRequestLogAsync writes: a keyed row carries no
	// owner_user_id (it resolves through the key), a keyless row carries the
	// request-time owner and no key.
	insert := func(vkID any, vkName, model string, ownerID any) string {
		var id string
		err := pool.QueryRow(context.Background(),
			`INSERT INTO request_logs (model_id, status_code, virtual_key_id, virtual_key_name, owner_user_id, created_at)
			 VALUES ($1, 200, $2, $3, $4, NOW()) RETURNING id`, model, vkID, vkName, ownerID).Scan(&id)
		if err != nil {
			t.Fatalf("insert log: %v", err)
		}
		return id
	}
	fx.aliceLogID = insert(aliceKey, "alice-key", "alice-model-1", nil)
	insert(aliceKey, "alice-key", "alice-model-2", nil)
	fx.bobLogID = insert(bobKey, "bob-key", "bob-model", nil)
	// Dashboard chat / arena rows: no virtual key, owner stamped at request time.
	fx.aliceChatLogID = insert(nil, "", "alice-chat-model", fx.aliceID)
	fx.bobChatLogID = insert(nil, "", "bob-chat-model", fx.bobID)
	// Pre-067 keyless row: no key and no owner, so it stays admin-only.
	insert(nil, "", "unattributed-model", nil)

	return fx
}

func listLogEntries(t *testing.T, router chi.Router, path, token string) []LogEntry {
	t.Helper()
	w := doJSON(t, router, http.MethodGet, path, token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	var resp struct {
		Entries []LogEntry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return resp.Entries
}

func TestLogs_OwnerScope_NonAdminSeesOnlyOwnTraffic(t *testing.T) {
	fx := setupLogScopeTest(t)

	// Alice owns two keyed rows plus one keyless chat row; bob one of each. The
	// unattributed keyless row belongs to neither.
	want := map[string]map[string]bool{
		fx.aliceToken: {"alice-model-1": true, "alice-model-2": true, "alice-chat-model": true},
		fx.bobToken:   {"bob-model": true, "bob-chat-model": true},
	}
	for _, path := range []string{"/logs?per_page=50", "/logs/cursor?limit=50"} {
		for token, models := range want {
			entries := listLogEntries(t, fx.router, path, token)
			if len(entries) != len(models) {
				t.Fatalf("%s: %d entries, want %d", path, len(entries), len(models))
			}
			for _, e := range entries {
				if !models[e.ModelID] {
					t.Errorf("%s leaked foreign row: %+v", path, e)
				}
			}
		}
	}
}

// TestLogs_OwnerScope_ChatRowsVisibleToTheirOwner is the gap this column
// closes: dashboard chat has no virtual key, so before request_logs.owner_user_id
// existed a non-admin's own chat traffic could not satisfy the key-join
// predicate and was invisible to them in the REST logs and stats.
func TestLogs_OwnerScope_ChatRowsVisibleToTheirOwner(t *testing.T) {
	fx := setupLogScopeTest(t)

	hasModel := func(entries []LogEntry, model string) bool {
		for _, e := range entries {
			if e.ModelID == model {
				return true
			}
		}
		return false
	}

	for _, path := range []string{"/logs?per_page=50", "/logs/cursor?limit=50"} {
		alice := listLogEntries(t, fx.router, path, fx.aliceToken)
		if !hasModel(alice, "alice-chat-model") {
			t.Errorf("%s: alice cannot see her own chat row", path)
		}
		if hasModel(alice, "bob-chat-model") {
			t.Errorf("%s: alice sees bob's chat row", path)
		}
		if hasModel(alice, "unattributed-model") {
			t.Errorf("%s: alice sees an ownerless keyless row", path)
		}
	}

	// The single-row fetch applies the same disjunction.
	if w := doJSON(t, fx.router, http.MethodGet, "/logs/"+fx.aliceChatLogID, fx.aliceToken, ""); w.Code != http.StatusOK {
		t.Errorf("own chat row: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, fx.router, http.MethodGet, "/logs/"+fx.bobChatLogID, fx.aliceToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("foreign chat row: %d, want 404", w.Code)
	}
}

// TestLogs_OwnerScope_KeyReassignmentMovesHistory guards the half of the
// predicate that did NOT change. Keyed rows resolve through the key's CURRENT
// owner, so reassigning a key hands its whole log history to the new owner.
// Widening the predicate to a disjunction must not have turned that into
// request-time attribution, and must not have widened what either user sees.
func TestLogs_OwnerScope_KeyReassignmentMovesHistory(t *testing.T) {
	fx := setupLogScopeTest(t)

	keys := listLogEntries(t, fx.router, "/logs/cursor?limit=50", fx.aliceToken)
	if len(keys) != 3 {
		t.Fatalf("alice before reassignment: %d entries, want 3", len(keys))
	}

	// Find alice's key and hand it to bob.
	w := doJSON(t, fx.router, http.MethodGet, "/virtual-keys", envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", w.Code, w.Body.String())
	}
	var listed []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode keys: %v", err)
	}
	aliceKeyID := ""
	for _, k := range listed {
		if k.Name == "alice-key" {
			aliceKeyID = k.ID
		}
	}
	if aliceKeyID == "" {
		t.Fatalf("alice-key not found in %+v", listed)
	}
	w = doJSON(t, fx.router, http.MethodPut, "/virtual-keys/"+aliceKeyID, envAdminToken,
		fmt.Sprintf(`{"name":"alice-key","owner_user_id":%q}`, fx.bobID))
	if w.Code != http.StatusOK {
		t.Fatalf("reassign key: %d %s", w.Code, w.Body.String())
	}
	globalLogsCache.clear()

	// Alice keeps only her keyless chat row; the two keyed rows followed the key.
	after := listLogEntries(t, fx.router, "/logs/cursor?limit=50", fx.aliceToken)
	if len(after) != 1 || after[0].ModelID != "alice-chat-model" {
		t.Fatalf("alice after reassignment: %+v, want only alice-chat-model", after)
	}
	if got := listLogEntries(t, fx.router, "/logs/cursor?limit=50", fx.bobToken); len(got) != 4 {
		t.Fatalf("bob after reassignment: %d entries, want 4", len(got))
	}
}

func TestLogs_OwnerScope_AdminSeesAllAndCanFilter(t *testing.T) {
	fx := setupLogScopeTest(t)

	if got := listLogEntries(t, fx.router, "/logs/cursor?limit=50", envAdminToken); len(got) != 6 {
		t.Fatalf("admin unfiltered: %d entries, want 6", len(got))
	}
	// The admin owner filter uses the same disjunction, so it picks up alice's
	// keyed rows and her chat row.
	filtered := listLogEntries(t, fx.router, "/logs/cursor?limit=50&owner_user_id="+fx.aliceID, envAdminToken)
	if len(filtered) != 3 {
		t.Fatalf("admin owner filter: %d entries, want 3", len(filtered))
	}
	// A malformed owner filter is ignored, like the other lenient filters.
	if got := listLogEntries(t, fx.router, "/logs/cursor?limit=50&owner_user_id=nonsense", envAdminToken); len(got) != 6 {
		t.Fatalf("admin bogus owner filter: %d entries, want 6", len(got))
	}
}

func TestLogs_OwnerScope_GetLog404OnForeignRow(t *testing.T) {
	fx := setupLogScopeTest(t)

	if w := doJSON(t, fx.router, http.MethodGet, "/logs/"+fx.aliceLogID, fx.aliceToken, ""); w.Code != http.StatusOK {
		t.Fatalf("own log: %d %s", w.Code, w.Body.String())
	}
	// Foreign row answers 404, indistinguishable from a nonexistent id.
	if w := doJSON(t, fx.router, http.MethodGet, "/logs/"+fx.bobLogID, fx.aliceToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign log: %d, want 404", w.Code)
	}
	if w := doJSON(t, fx.router, http.MethodGet, "/logs/"+fx.bobLogID, envAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("admin fetch: %d", w.Code)
	}
}

func TestLogs_OwnerScope_CacheDoesNotLeakAcrossIdentities(t *testing.T) {
	fx := setupLogScopeTest(t)

	// Prime the offset-list cache as admin, then request the byte-identical
	// query as a scoped user: the cache key carries the owner scope, so alice
	// must not be served the admin's 4-row page.
	if got := listLogEntries(t, fx.router, "/logs?per_page=50", envAdminToken); len(got) != 6 {
		t.Fatalf("admin prime: %d entries, want 6", len(got))
	}
	if got := listLogEntries(t, fx.router, "/logs?per_page=50", fx.aliceToken); len(got) != 3 {
		t.Fatalf("alice after admin prime: %d entries, want 3 (cache leak?)", len(got))
	}
}

func TestStats_OwnerScope(t *testing.T) {
	fx := setupLogScopeTest(t)

	getStats := func(path, token string) StatsResponse {
		w := doJSON(t, fx.router, http.MethodGet, path, token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		var s StatsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatalf("decode stats: %v", err)
		}
		return s
	}

	// Alice sees her two keyed requests plus her own chat request; the by-key
	// breakdown never names a foreign key.
	s := getStats("/stats?period=7d", fx.aliceToken)
	if s.TotalRequestsLast7d != 3 {
		t.Errorf("alice total7d = %d, want 3", s.TotalRequestsLast7d)
	}
	if _, leaked := s.ByVirtualKey["bob-key"]; leaked {
		t.Error("alice by_virtual_key leaked bob-key")
	}
	if s.ByVirtualKey["alice-key"] != 2 {
		t.Errorf("alice by_virtual_key[alice-key] = %d, want 2", s.ByVirtualKey["alice-key"])
	}

	// Admin is unscoped (6 rows incl. the ownerless keyless one) and can filter.
	if s := getStats("/stats?period=7d", envAdminToken); s.TotalRequestsLast7d != 6 {
		t.Errorf("admin total7d = %d, want 6", s.TotalRequestsLast7d)
	}
	if s := getStats("/stats?period=7d&owner_user_id="+fx.bobID, envAdminToken); s.TotalRequestsLast7d != 2 {
		t.Errorf("admin owner-filtered total7d = %d, want 2", s.TotalRequestsLast7d)
	}
}

// TestStats_OwnerScope_ScalarsAndLatency covers the two owner-scoped stats
// helpers whose failures are silent: statScalars and statLatencyBreakdown both
// log and zero their fields rather than returning an error, so a wrong bind
// index in either would hand scoped users an empty latency chart and zeroed
// scalars while every other assertion stayed green.
func TestStats_OwnerScope_ScalarsAndLatency(t *testing.T) {
	router, loginAs, mkUser := setupOwnershipTest(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs`); err != nil {
		t.Fatalf("truncate request_logs: %v", err)
	}
	globalLogsCache.clear()

	aliceID := mkUser("latency-alice", []string{string(user.GrantLogs), string(user.GrantUsage)})
	aliceToken := loginAs(aliceID)

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "latency-provider", "https://api.example.com/v1")

	// The per-provider breakdown needs at least 3 rows per provider (HAVING
	// COUNT(*) >= 3), so give alice exactly 3 and leave 3 unowned: scoped to
	// alice the provider still clears the bar, so an empty result means the
	// scoped query broke rather than the threshold filtering it out.
	insertOwned := func(model string, owner any) {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, owner_user_id, created_at)
			 VALUES ($1, $2, 200, 500, 20, 100, 50, $3, NOW())`, providerID, model, owner)
		if err != nil {
			t.Fatalf("insert log: %v", err)
		}
	}
	for i := range 3 {
		insertOwned(fmt.Sprintf("alice-model-%d", i), aliceID)
		insertOwned(fmt.Sprintf("unowned-model-%d", i), nil)
	}

	getStats := func(token string) StatsResponse {
		w := doJSON(t, router, http.MethodGet, "/stats?period=7d&include_latency=true", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET stats: %d %s", w.Code, w.Body.String())
		}
		var s StatsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatalf("decode stats: %v", err)
		}
		return s
	}

	alice := getStats(aliceToken)
	// statScalars, scoped: the 1h count builds its own arg list, separate from
	// the other scalar queries, so it is asserted explicitly.
	if alice.RequestsLast1h != 3 {
		t.Errorf("alice requests_last_1h = %d, want 3", alice.RequestsLast1h)
	}
	if alice.TotalTokensPrompt != 300 || alice.TotalTokensCompletion != 150 {
		t.Errorf("alice tokens = %d/%d, want 300/150",
			alice.TotalTokensPrompt, alice.TotalTokensCompletion)
	}
	if alice.AvgLatencyMs != 500 {
		t.Errorf("alice avg_latency_ms = %v, want 500", alice.AvgLatencyMs)
	}
	if alice.AvgOverheadMs != 20 {
		t.Errorf("alice avg_overhead_ms = %v, want 20", alice.AvgOverheadMs)
	}
	// statLatencyBreakdown, scoped.
	if len(alice.ByProviderLatency) != 1 {
		t.Fatalf("alice by_provider_latency = %d entries, want 1", len(alice.ByProviderLatency))
	}
	if got := alice.ByProviderLatency[0]; got.ProviderName != "latency-provider" || got.RequestCount != 3 {
		t.Errorf("alice latency entry = %s/%d, want latency-provider/3", got.ProviderName, got.RequestCount)
	}

	// Admin is unscoped and sees all six rows through the same helpers.
	admin := getStats(envAdminToken)
	if admin.RequestsLast1h != 6 {
		t.Errorf("admin requests_last_1h = %d, want 6", admin.RequestsLast1h)
	}
	if len(admin.ByProviderLatency) != 1 || admin.ByProviderLatency[0].RequestCount != 6 {
		t.Errorf("admin latency entry = %+v, want one entry counting 6", admin.ByProviderLatency)
	}
}

func TestStats_TimeSeries_OwnerScope(t *testing.T) {
	fx := setupLogScopeTest(t)

	sumCounts := func(token, path string) int {
		w := doJSON(t, fx.router, http.MethodGet, path, token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		var ts TimeSeriesStats
		if err := json.Unmarshal(w.Body.Bytes(), &ts); err != nil {
			t.Fatalf("decode timeseries: %v", err)
		}
		total := 0
		for _, p := range ts.Points {
			total += p.Count
		}
		return total
	}

	if got := sumCounts(fx.aliceToken, "/stats/timeseries"); got != 3 {
		t.Errorf("alice timeseries total = %d, want 3", got)
	}
	if got := sumCounts(envAdminToken, "/stats/timeseries"); got != 6 {
		t.Errorf("admin timeseries total = %d, want 6", got)
	}
	if got := sumCounts(envAdminToken, "/stats/timeseries?owner_user_id="+fx.aliceID); got != 3 {
		t.Errorf("admin filtered timeseries total = %d, want 3", got)
	}

	// The provider-distribution path applies the same scope (fixture rows have
	// no provider, so everyone sees an empty set; this just exercises the
	// scoped query shape end to end).
	w := doJSON(t, fx.router, http.MethodGet, "/stats/provider-distribution", fx.aliceToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("provider-distribution: %d %s", w.Code, w.Body.String())
	}
}
