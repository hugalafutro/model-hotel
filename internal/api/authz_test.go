package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// The allowed_providers column has no FK to providers (it is a plain
// TEXT[]; see 065_user_allowed_providers.sql), so — matching the sibling
// cap tests in users_allowed_providers_test.go — this uses a literal
// provider name rather than seeding a real providers row.
func TestMe_ReportsProviderCap(t *testing.T) {
	router, loginAs, _ := setupOwnershipTest(t)

	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["prov-a"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	w = doJSONAs(t, router, http.MethodGet, "/auth/me", loginAs, &alice, "")
	if w.Code != http.StatusOK {
		t.Fatalf("me: %d %s", w.Code, w.Body.String())
	}
	var me struct {
		AllowedProviders *[]string `json:"allowed_providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.AllowedProviders == nil || len(*me.AllowedProviders) != 1 || (*me.AllowedProviders)[0] != "prov-a" {
		t.Fatalf("allowed_providers = %v, want [prov-a]", me.AllowedProviders)
	}
}

// An uncapped account (nil AllowedProviders) must report the field as
// entirely absent from the JSON payload (omitempty on a nil pointer), not
// as null or an empty list — either of those would read as "capped to
// nothing" to a client that only checks for key presence.
func TestMe_UncappedAccountOmitsAllowedProviders(t *testing.T) {
	router, loginAs, mkUser := setupOwnershipTest(t)
	uid := mkUser("bob", []string{string(user.GrantVirtualKeys)})
	parsed, err := uuid.Parse(uid)
	if err != nil {
		t.Fatalf("parse uid: %v", err)
	}
	bob := &user.User{ID: parsed}

	w := doJSONAs(t, router, http.MethodGet, "/auth/me", loginAs, bob, "")
	if w.Code != http.StatusOK {
		t.Fatalf("me: %d %s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, present := raw["allowed_providers"]; present {
		t.Fatalf("allowed_providers present in response for uncapped user: %s", w.Body.String())
	}
}
