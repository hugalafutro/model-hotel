package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// An omitted allowed_providers must PRESERVE the stored cap on update, while an
// explicit null clears it. Getting this backwards is the exact bug class that
// produced the reported virtual-key bypass.
func TestUpdateUser_AllowedProvidersOmittedVsNull(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"capped","password":"password123","role":"user","grants":[],"allowed_providers":["p1","p2"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created user.User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.AllowedProviders == nil || len(*created.AllowedProviders) != 2 {
		t.Fatalf("create did not persist the cap: %+v", created.AllowedProviders)
	}

	// Omitted: cap survives.
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"capped","role":"user","grants":[],"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var kept user.User
	if err := json.Unmarshal(w.Body.Bytes(), &kept); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kept.AllowedProviders == nil || len(*kept.AllowedProviders) != 2 {
		t.Fatalf("omitted allowed_providers dropped the cap: %+v", kept.AllowedProviders)
	}

	// Explicit null: cap cleared.
	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"capped","role":"user","grants":[],"enabled":true,"allowed_providers":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update null: %d %s", w.Code, w.Body.String())
	}
	var cleared user.User
	if err := json.Unmarshal(w.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cleared.AllowedProviders != nil {
		t.Fatalf("explicit null did not clear the cap: %+v", *cleared.AllowedProviders)
	}
}

func TestUser_AllowedProvidersEmptyArrayRejected(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"empty","password":"password123","role":"user","grants":[],"allowed_providers":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
	}
}

// The empty-array rejection is correct by construction today (both handlers
// call validate() before touching the repository), but that is an
// implementation detail a future refactor could silently break. Assert the
// PUT path directly so it stays pinned.
func TestUpdateUser_AllowedProvidersEmptyArrayRejected(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"emptyupdate","password":"password123","role":"user","grants":[]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created user.User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	w = doJSON(t, router, http.MethodPut, "/users/"+created.ID.String(), envAdminToken,
		`{"username":"emptyupdate","role":"user","grants":[],"enabled":true,"allowed_providers":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
	}
}
