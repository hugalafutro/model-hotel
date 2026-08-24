package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// The provider list's total_tokens is owner-scoped for non-admins: it applies
// the same two-shape owner predicate as the logs/stats surfaces (keyed rows
// through the key's current owner, keyless chat rows through the request-time
// owner_user_id), so a usage-granted user reads only their own aggregate
// volume while an admin still sees the fleet-wide total.
func TestListProviders_TotalTokensOwnerScoped(t *testing.T) {
	router, loginAs, mkUser := setupOwnershipTest(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs`); err != nil {
		t.Fatalf("truncate request_logs: %v", err)
	}

	w := doJSON(t, router, http.MethodPost, "/providers", envAdminToken,
		`{"name":"scope-provider","base_url":"https://api.example.com","api_key":"sk-scope"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	aliceID := mkUser("prov-alice", []string{string(user.GrantUsage)})
	bobID := mkUser("prov-bob", []string{string(user.GrantUsage)})
	aliceToken := loginAs(aliceID)
	bobToken := loginAs(bobID)

	mkKey := func(name, owner string) string {
		w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
			fmt.Sprintf(`{"name":%q,"owner_user_id":%q}`, name, owner))
		if w.Code != http.StatusCreated {
			t.Fatalf("create key %s: %d %s", name, w.Code, w.Body.String())
		}
		return decodeVK(t, w.Body.Bytes()).ID
	}
	aliceKey := mkKey("prov-alice-key", aliceID)
	bobKey := mkKey("prov-bob-key", bobID)

	insert := func(vkID any, vkName string, ownerID any, prompt, completion int) {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO request_logs (model_id, status_code, virtual_key_id, virtual_key_name, owner_user_id, provider_id, tokens_prompt, tokens_completion, created_at)
			 VALUES ($1, 200, $2, $3, $4, $5, $6, $7, NOW())`,
			"scope-model", vkID, vkName, ownerID, created.ID, prompt, completion)
		if err != nil {
			t.Fatalf("insert log: %v", err)
		}
	}
	// Keyed rows resolve their owner through the key.
	insert(aliceKey, "prov-alice-key", nil, 100, 200)
	insert(bobKey, "prov-bob-key", nil, 300, 400)
	// Keyless dashboard-chat row: owner stamped on the row itself.
	insert(nil, "", aliceID, 7, 13)
	// Pre-067 keyless row with no owner: counted for admins only.
	insert(nil, "", nil, 1000, 1000)
	// A row behind a key that is then deleted: the dangling virtual_key_id no
	// longer resolves an owner, so its tokens leave bob's total and stay
	// visible to admins only (same semantics as the logs/stats surfaces).
	doomedKey := mkKey("prov-doomed-key", bobID)
	insert(doomedKey, "prov-doomed-key", nil, 50, 50)
	if w := doJSON(t, router, http.MethodDelete, "/virtual-keys/"+doomedKey, envAdminToken, ""); w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("delete doomed key: %d %s", w.Code, w.Body.String())
	}

	totalFor := func(token string) int {
		t.Helper()
		w := doJSON(t, router, http.MethodGet, "/providers", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET /providers: %d %s", w.Code, w.Body.String())
		}
		var resp []struct {
			ID          string `json:"id"`
			TotalTokens int    `json:"total_tokens"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode providers: %v", err)
		}
		for _, p := range resp {
			if p.ID == created.ID {
				return p.TotalTokens
			}
		}
		t.Fatalf("provider %s missing from list", created.ID)
		return 0
	}

	if got := totalFor(aliceToken); got != 320 {
		t.Errorf("alice total_tokens = %d, want 320 (own keyed 300 + own chat 20)", got)
	}
	if got := totalFor(bobToken); got != 700 {
		t.Errorf("bob total_tokens = %d, want 700 (own keyed traffic only, deleted-key row excluded)", got)
	}
	if got := totalFor(envAdminToken); got != 3120 {
		t.Errorf("admin total_tokens = %d, want 3120 (all owners, unattributed, and deleted-key rows)", got)
	}
}
