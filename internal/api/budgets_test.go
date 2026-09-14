package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/budget"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// seedCostRow writes one priced request_logs row for a key or a keyless owner.
func seedCostRow(t *testing.T, vkID, ownerID *string, cost *float64, at time.Time) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(), `INSERT INTO request_logs (id, model_id, status_code, duration_ms, cost_usd, virtual_key_id, owner_user_id, created_at)
		VALUES (gen_random_uuid(), 'm1', 200, 10, $1, $2, $3, $4)`, cost, vkID, ownerID, at); err != nil {
		t.Fatalf("seed cost row: %v", err)
	}
}

func TestPGSource_SumsKeyRowsAndOwnerRowsSincePeriodStart(t *testing.T) {
	router, _, mkUser := setupOwnershipTest(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs`); err != nil {
		t.Fatal(err)
	}
	ownerID := mkUser("spender", []string{"virtual_keys"})
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, `{"name":"k-sum","owner_user_id":"`+ownerID+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	vk := decodeVK(t, w.Body.Bytes())
	f := func(v float64) *float64 { return &v }
	now := time.Now().UTC()
	seedCostRow(t, &vk.ID, &ownerID, f(1.5), now)                    // the key's own row, stamped with its owner
	seedCostRow(t, nil, &ownerID, f(2), now)                         // keyless dashboard chat by the owner
	seedCostRow(t, &vk.ID, &ownerID, nil, now)                       // unpriced: counts as nothing
	seedCostRow(t, &vk.ID, &ownerID, f(100), now.Add(-48*time.Hour)) // last period

	src := budget.PGSource{Pool: pool}
	since := now.Add(-time.Hour)
	if got, err := src.Spend(context.Background(), budget.KindKey, vk.ID, since); err != nil || got != 1.5 {
		t.Fatalf("key spend = %v, %v; want 1.5", got, err)
	}
	if got, err := src.Spend(context.Background(), budget.KindUser, ownerID, since); err != nil || got != 3.5 {
		t.Fatalf("user spend = %v, %v; want 3.5 (own row and the key's)", got, err)
	}
	// The stamp is what keeps the spend with who spent it: deleting the key
	// (a user with the virtual_keys grant can, and recreate it) changes
	// nothing about the owner's sum.
	if w := doJSON(t, router, http.MethodDelete, "/virtual-keys/"+vk.ID, envAdminToken, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete key: %d", w.Code)
	}
	if got, err := src.Spend(context.Background(), budget.KindUser, ownerID, since); err != nil || got != 3.5 {
		t.Fatalf("user spend after deleting the key = %v, %v; want 3.5", got, err)
	}
}

func TestUsersAPI_ListCarriesBudgetSpent(t *testing.T) {
	h, router, _, mkUser := setupOwnershipHandler(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs`); err != nil {
		t.Fatal(err)
	}
	h.SetBudgetLimiter(budget.NewLimiter(budget.PGSource{Pool: pool}))
	id := mkUser("spender", []string{"chat"})
	if _, err := pool.Exec(context.Background(), `UPDATE users SET budget_usd = 20, budget_period = 'month' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }
	seedCostRow(t, nil, &id, f(4.5), time.Now().UTC())

	w := doJSON(t, router, http.MethodGet, "/users", envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var users []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, u := range users {
		if u["username"] == "spender" {
			if u["budget_spent_usd"] != 4.5 {
				t.Fatalf("budget_spent_usd = %v, want 4.5", u["budget_spent_usd"])
			}
			return
		}
	}
	t.Fatal("spender not listed")
}

func TestVirtualKeysAPI_BudgetFields(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE request_logs, virtual_keys CASCADE`); err != nil {
		t.Fatal(err)
	}
	h.SetBudgetLimiter(budget.NewLimiter(budget.PGSource{Pool: pool}))

	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, `{"name":"k-budget","budget_usd":25,"budget_period":"month"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())
	if created.BudgetUSD == nil || *created.BudgetUSD != 25 || created.BudgetPeriod == nil || *created.BudgetPeriod != "month" {
		t.Fatalf("budget not persisted: %+v", created)
	}

	f := func(v float64) *float64 { return &v }
	seedCostRow(t, &created.ID, nil, f(3.25), time.Now().UTC())
	w = doJSON(t, router, http.MethodGet, "/virtual-keys/"+created.ID, envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	got := decodeVK(t, w.Body.Bytes())
	if got.BudgetSpentUSD == nil || *got.BudgetSpentUSD != 3.25 {
		t.Fatalf("budget_spent_usd = %v, want 3.25", got.BudgetSpentUSD)
	}
	w = doJSON(t, router, http.MethodGet, "/virtual-keys", envAdminToken, "")
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %s", err, w.Body.String())
	}
	if list[0]["budget_spent_usd"] != 3.25 {
		t.Errorf("list budget_spent_usd = %v, want 3.25", list[0]["budget_spent_usd"])
	}

	// An update that never mentions the budget keeps it (a caller that
	// predates the field must not drop a spending guard); an explicit null
	// pair clears it, and the spend field goes with it.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken, `{"name":"k-budget-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if kept := decodeVK(t, w.Body.Bytes()); kept.BudgetUSD == nil || *kept.BudgetUSD != 25 || kept.BudgetPeriod == nil || *kept.BudgetPeriod != "month" {
		t.Errorf("an omitted budget must be preserved: %+v", kept)
	}
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken, `{"name":"k-budget","budget_usd":null,"budget_period":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	cleared := decodeVK(t, w.Body.Bytes())
	if cleared.BudgetUSD != nil || cleared.BudgetPeriod != nil || cleared.BudgetSpentUSD != nil {
		t.Errorf("budget survived a null update: %+v", cleared)
	}

	// Half a pair, a zero and an unknown period are refused on both writes.
	for _, body := range []string{
		`{"name":"k-bad","budget_usd":5}`,
		`{"name":"k-bad","budget_period":"day"}`,
		`{"name":"k-bad","budget_usd":0,"budget_period":"day"}`,
		`{"name":"k-bad","budget_usd":5,"budget_period":"quarter"}`,
	} {
		if w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, body); w.Code != http.StatusBadRequest {
			t.Errorf("create %s: %d, want 400", body, w.Code)
		}
		if w := doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken, body); w.Code != http.StatusBadRequest {
			t.Errorf("update %s: %d, want 400", body, w.Code)
		}
	}
}

func TestUsersAPI_BudgetFields(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"budgeted","password":"password123","role":"user","grants":[],"budget_usd":12.5,"budget_period":"week"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created user.User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.BudgetUSD == nil || *created.BudgetUSD != 12.5 || created.BudgetPeriod == nil || *created.BudgetPeriod != "week" {
		t.Fatalf("budget not persisted: %+v", created)
	}
	// An update that never mentions the budget keeps it: a caller that
	// predates the field must not drop a spending guard. An explicit null
	// pair clears it.
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"budgeted","role":"user","grants":[],"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated user.User
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.BudgetUSD == nil || *updated.BudgetUSD != 12.5 || updated.BudgetPeriod == nil || *updated.BudgetPeriod != "week" {
		t.Errorf("an omitted budget must be preserved: %+v", updated)
	}
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"budgeted","role":"user","grants":[],"enabled":true,"budget_usd":null,"budget_period":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.BudgetUSD != nil || updated.BudgetPeriod != nil {
		t.Errorf("an explicit null pair must clear the budget: %+v", updated)
	}
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"budgeted","role":"user","grants":[],"enabled":true,"budget_usd":-1,"budget_period":"day"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("negative budget: %d, want 400", w.Code)
	}
}

func TestConfigSync_BudgetsRoundTrip(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	seedUser(t, "budgetowner", nil, true, []string{"virtual_keys"})
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		UPDATE users SET budget_usd = 40, budget_period = 'month' WHERE username = 'budgetowner'`); err != nil {
		t.Fatal(err)
	}
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview, budget_usd, budget_period)
		VALUES ('budget-key', 'hash-budget-sync', 'sk-...bs', 2.5, 'day')`); err != nil {
		t.Fatal(err)
	}

	env := doExport(t, r)
	if len(env.Config.Users) != 1 || len(env.Config.VirtualKeys) != 1 {
		t.Fatalf("export carried %d users / %d keys, want 1/1", len(env.Config.Users), len(env.Config.VirtualKeys))
	}
	if u := env.Config.Users[0]; u.BudgetUSD == nil || *u.BudgetUSD != 40 || u.BudgetPeriod == nil || *u.BudgetPeriod != "month" {
		t.Fatalf("export dropped the user budget: %+v", u)
	}
	if vk := env.Config.VirtualKeys[0]; vk.BudgetUSD == nil || *vk.BudgetUSD != 2.5 || vk.BudgetPeriod == nil || *vk.BudgetPeriod != "day" {
		t.Fatalf("export dropped the key budget: %+v", vk)
	}

	cleanConfigTables(t)
	cleanUsersTable(t)
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var uUSD, kUSD *float64
	var uPeriod, kPeriod *string
	if err := apiTestDB.Pool().QueryRow(context.Background(), `
		SELECT u.budget_usd, u.budget_period, vk.budget_usd, vk.budget_period
		FROM users u, virtual_keys vk WHERE u.username = 'budgetowner' AND vk.key_hash = 'hash-budget-sync'`).
		Scan(&uUSD, &uPeriod, &kUSD, &kPeriod); err != nil {
		t.Fatal(err)
	}
	if uUSD == nil || *uUSD != 40 || uPeriod == nil || *uPeriod != "month" || kUSD == nil || *kUSD != 2.5 || kPeriod == nil || *kPeriod != "day" {
		t.Errorf("member budgets did not converge: user %v/%v key %v/%v", uUSD, uPeriod, kUSD, kPeriod)
	}

	// An envelope carrying half a pair is refused as a whole.
	env.Config.VirtualKeys[0].BudgetPeriod = nil
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("half-pair import status = %d, want 400", rec.Code)
	}
}
