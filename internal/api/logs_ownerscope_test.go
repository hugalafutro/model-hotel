package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// logScopeFixture is the harness state for the owner-scope tests: two
// log-granted users, one owned virtual key each, and four request_logs rows
// (two for alice, one for bob, one with no virtual key at all).
type logScopeFixture struct {
	router               chi.Router
	aliceToken, bobToken string
	aliceID, bobID       string
	aliceLogID, bobLogID string
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
	fx.aliceID = mkUser("log-alice", []string{string(user.GrantLogs)})
	fx.bobID = mkUser("log-bob", []string{string(user.GrantLogs)})
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

	insert := func(vkID any, vkName, model string) string {
		var id string
		err := pool.QueryRow(context.Background(),
			`INSERT INTO request_logs (model_id, status_code, virtual_key_id, virtual_key_name, created_at)
			 VALUES ($1, 200, $2, $3, NOW()) RETURNING id`, model, vkID, vkName).Scan(&id)
		if err != nil {
			t.Fatalf("insert log: %v", err)
		}
		return id
	}
	fx.aliceLogID = insert(aliceKey, "alice-key", "alice-model-1")
	insert(aliceKey, "alice-key", "alice-model-2")
	fx.bobLogID = insert(bobKey, "bob-key", "bob-model")
	insert(nil, "", "unkeyed-model") // admin chat / arena style row

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

	for _, path := range []string{"/logs?per_page=50", "/logs/cursor?limit=50"} {
		entries := listLogEntries(t, fx.router, path, fx.aliceToken)
		if len(entries) != 2 {
			t.Fatalf("%s as alice: %d entries, want 2", path, len(entries))
		}
		for _, e := range entries {
			if e.VirtualKeyName != "alice-key" {
				t.Errorf("%s leaked foreign row: %+v", path, e)
			}
		}
		if got := listLogEntries(t, fx.router, path, fx.bobToken); len(got) != 1 {
			t.Fatalf("%s as bob: %d entries, want 1", path, len(got))
		}
	}
}

func TestLogs_OwnerScope_AdminSeesAllAndCanFilter(t *testing.T) {
	fx := setupLogScopeTest(t)

	if got := listLogEntries(t, fx.router, "/logs/cursor?limit=50", envAdminToken); len(got) != 4 {
		t.Fatalf("admin unfiltered: %d entries, want 4", len(got))
	}
	filtered := listLogEntries(t, fx.router, "/logs/cursor?limit=50&owner_user_id="+fx.aliceID, envAdminToken)
	if len(filtered) != 2 {
		t.Fatalf("admin owner filter: %d entries, want 2", len(filtered))
	}
	// A malformed owner filter is ignored, like the other lenient filters.
	if got := listLogEntries(t, fx.router, "/logs/cursor?limit=50&owner_user_id=nonsense", envAdminToken); len(got) != 4 {
		t.Fatalf("admin bogus owner filter: %d entries, want 4", len(got))
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
	if got := listLogEntries(t, fx.router, "/logs?per_page=50", envAdminToken); len(got) != 4 {
		t.Fatalf("admin prime: %d entries, want 4", len(got))
	}
	if got := listLogEntries(t, fx.router, "/logs?per_page=50", fx.aliceToken); len(got) != 2 {
		t.Fatalf("alice after admin prime: %d entries, want 2 (cache leak?)", len(got))
	}
}
