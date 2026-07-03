package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// setupOwnershipTest wires the multi-user stack (mirroring setupUsersTest)
// plus a clean virtual_keys table, returning session-token and user-creation
// helpers.
func setupOwnershipTest(t *testing.T) (router chi.Router, loginAs func(id string) string, mkUser func(name string, grants []string) string) {
	t.Helper()
	h, router := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions, virtual_keys CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)
	loginAs = func(id string) string {
		token, err := sessionMgr.CreateAuthToken(context.Background(), []byte(id), nil)
		if err != nil {
			t.Fatalf("CreateAuthToken: %v", err)
		}
		return token
	}
	mkUser = func(name string, grants []string) string {
		g, _ := json.Marshal(grants)
		w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
			fmt.Sprintf(`{"username":%q,"password":"password123","role":"user","grants":%s}`, name, g))
		if w.Code != http.StatusCreated {
			t.Fatalf("create user %s: %d %s", name, w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode user: %v", err)
		}
		return resp.ID
	}
	return router, loginAs, mkUser
}

func decodeVK(t *testing.T, body []byte) virtualkey.VirtualKeyResponse {
	t.Helper()
	var resp virtualkey.VirtualKeyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode virtual key: %v", err)
	}
	return resp
}

func TestVirtualKeysAPI_AdminAssignsOwner(t *testing.T) {
	router, _, mkUser := setupOwnershipTest(t)
	uid := mkUser("vk-owner-admin-flow", []string{string(user.GrantVirtualKeys)})

	// Create with an owner.
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		fmt.Sprintf(`{"name":"owned-key","owner_user_id":%q}`, uid))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())
	if created.OwnerUserID == nil || *created.OwnerUserID != uid {
		t.Fatalf("owner_user_id = %v, want %s", created.OwnerUserID, uid)
	}
	if created.OwnerUsername == nil || *created.OwnerUsername != "vk-owner-admin-flow" {
		t.Errorf("owner_username = %v, want vk-owner-admin-flow", created.OwnerUsername)
	}

	// Update omitting owner_user_id preserves the owner.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"owned-key-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if updated := decodeVK(t, w.Body.Bytes()); updated.OwnerUserID == nil || *updated.OwnerUserID != uid {
		t.Errorf("owner lost on omitted-field update: %v", updated.OwnerUserID)
	}

	// Explicit null clears the owner.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"owned-key-renamed","owner_user_id":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update(null owner): %d %s", w.Code, w.Body.String())
	}
	if cleared := decodeVK(t, w.Body.Bytes()); cleared.OwnerUserID != nil {
		t.Errorf("owner not cleared: %v", cleared.OwnerUserID)
	}
}

func TestVirtualKeysAPI_CreateRejectsBadOwner(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	// Not a UUID.
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"bad-owner","owner_user_id":"not-a-uuid"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-uuid owner: %d, want 400", w.Code)
	}

	// A valid UUID with no matching users row (FK violation).
	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"ghost-owner","owner_user_id":"00000000-0000-0000-0000-000000000001"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown owner: %d %s, want 400", w.Code, w.Body.String())
	}
}

func TestVirtualKeysAPI_UpdateRejectsBadOwner(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	// A plain unowned key to reassign.
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, `{"name":"reassign-me"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := decodeVK(t, w.Body.Bytes()).ID

	// Reassigning to a malformed UUID is a 400 (resolveWriteOwner parse error).
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+id, envAdminToken,
		`{"name":"reassign-me","owner_user_id":"not-a-uuid"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("update to non-uuid owner: %d, want 400", w.Code)
	}

	// Reassigning to a valid-but-unknown UUID is a 400 (FK violation surfaced
	// from the update statement).
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+id, envAdminToken,
		`{"name":"reassign-me","owner_user_id":"00000000-0000-0000-0000-000000000001"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("update to ghost owner: %d %s, want 400", w.Code, w.Body.String())
	}
}

func TestVirtualKeysAPI_NonAdminScopedToOwnKeys(t *testing.T) {
	router, loginAs, mkUser := setupOwnershipTest(t)
	aliceID := mkUser("vk-alice", []string{string(user.GrantVirtualKeys)})
	bobID := mkUser("vk-bob", []string{string(user.GrantVirtualKeys)})
	alice := loginAs(aliceID)
	bob := loginAs(bobID)

	// Alice creates a key: it is forced to be hers even if she claims
	// someone else's ownership in the body.
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", alice,
		fmt.Sprintf(`{"name":"alice-key","owner_user_id":%q}`, bobID))
	if w.Code != http.StatusCreated {
		t.Fatalf("alice create: %d %s", w.Code, w.Body.String())
	}
	aliceKey := decodeVK(t, w.Body.Bytes())
	if aliceKey.OwnerUserID == nil || *aliceKey.OwnerUserID != aliceID {
		t.Fatalf("alice's key owner = %v, want %s (self, body ignored)", aliceKey.OwnerUserID, aliceID)
	}

	// An unowned admin key exists alongside.
	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, `{"name":"shared-key"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create: %d", w.Code)
	}
	sharedKey := decodeVK(t, w.Body.Bytes())

	// Alice lists only her own key; the admin sees both.
	w = doJSON(t, router, http.MethodGet, "/virtual-keys", alice, "")
	if w.Code != http.StatusOK {
		t.Fatalf("alice list: %d", w.Code)
	}
	var aliceList []virtualkey.VirtualKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &aliceList); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(aliceList) != 1 || aliceList[0].ID != aliceKey.ID {
		t.Fatalf("alice list = %d keys, want exactly her own", len(aliceList))
	}
	w = doJSON(t, router, http.MethodGet, "/virtual-keys", envAdminToken, "")
	var adminList []virtualkey.VirtualKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &adminList); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if len(adminList) != 2 {
		t.Fatalf("admin list = %d keys, want 2", len(adminList))
	}

	// Bob cannot see, edit, or delete Alice's key: uniformly 404.
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/virtual-keys/" + aliceKey.ID, ""},
		{http.MethodPut, "/virtual-keys/" + aliceKey.ID, `{"name":"stolen"}`},
		{http.MethodDelete, "/virtual-keys/" + aliceKey.ID, ""},
	} {
		w = doJSON(t, router, tc.method, tc.path, bob, tc.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s as bob: %d, want 404", tc.method, tc.path, w.Code)
		}
	}
	// Same for the unowned key: unowned means admin-only.
	w = doJSON(t, router, http.MethodGet, "/virtual-keys/"+sharedKey.ID, bob, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET unowned key as bob: %d, want 404", w.Code)
	}

	// Deleting a key that does not exist at all is also a 404 for a non-admin
	// (the ownership pre-read reports the absent row as missing).
	w = doJSON(t, router, http.MethodDelete, "/virtual-keys/00000000-0000-0000-0000-0000000000ff", bob, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE missing key as bob: %d, want 404", w.Code)
	}

	// Alice can rename her key, and cannot give it away or orphan it.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+aliceKey.ID, alice,
		fmt.Sprintf(`{"name":"alice-renamed","owner_user_id":%q}`, bobID))
	if w.Code != http.StatusOK {
		t.Fatalf("alice update: %d %s", w.Code, w.Body.String())
	}
	if renamed := decodeVK(t, w.Body.Bytes()); renamed.OwnerUserID == nil || *renamed.OwnerUserID != aliceID {
		t.Errorf("alice's update changed owner: %v", renamed.OwnerUserID)
	}

	// And she can delete it.
	w = doJSON(t, router, http.MethodDelete, "/virtual-keys/"+aliceKey.ID, alice, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("alice delete: %d", w.Code)
	}
}

func TestUsersAPI_LimitFields(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	// Create with limits.
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"limited","password":"password123","role":"user","grants":[],"rate_limit_rps":2.5,"rate_limit_burst":4,"rate_limit_tpm":6000}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created user.User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.RateLimitRPS == nil || *created.RateLimitRPS != 2.5 ||
		created.RateLimitBurst == nil || *created.RateLimitBurst != 4 ||
		created.RateLimitTPM == nil || *created.RateLimitTPM != 6000 {
		t.Fatalf("limits not persisted: %+v", created)
	}

	// Update clears RPS/burst (omitted), keeps a new TPM.
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"limited","role":"user","grants":[],"enabled":true,"rate_limit_tpm":9000}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated user.User
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.RateLimitRPS != nil || updated.RateLimitBurst != nil {
		t.Errorf("omitted limits survived: %+v", updated)
	}
	if updated.RateLimitTPM == nil || *updated.RateLimitTPM != 9000 {
		t.Errorf("rate_limit_tpm = %v, want 9000", updated.RateLimitTPM)
	}

	// Invalid limits are rejected on both create and update.
	w = doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"badlimits","password":"password123","role":"user","grants":[],"rate_limit_tpm":0}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("create with tpm=0: %d, want 400", w.Code)
	}
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"limited","role":"user","grants":[],"enabled":true,"rate_limit_rps":-1}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("update with rps=-1: %d, want 400", w.Code)
	}
}
