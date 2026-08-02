package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A capped user must round-trip with the cap intact: exported by provider NAME
// (UUIDs are instance-local) and resolved back to this member's UUID on import.
func TestConfigSync_UserCapRoundTrip(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	id := seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	seedUser(t, "alice", nil, true, []string{"virtual_keys"})
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE users SET allowed_providers = $1 WHERE username = 'alice'`, []string{id}); err != nil {
		t.Fatalf("set cap: %v", err)
	}

	env := doExport(t, r)
	// Track the subject explicitly: a loop that never matches would otherwise
	// pass this test vacuously if the export dropped the user entirely.
	found := false
	for _, u := range env.Config.Users {
		if u.Username != "alice" {
			continue
		}
		found = true
		if u.AllowedProviderNames == nil || len(*u.AllowedProviderNames) != 1 ||
			(*u.AllowedProviderNames)[0] != "prov-a" {
			t.Fatalf("exported cap = %v, want [prov-a]", u.AllowedProviderNames)
		}
	}
	if !found {
		t.Fatal("export did not carry user alice")
	}

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []string
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT allowed_providers FROM users WHERE username = 'alice'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) != 1 || got[0] != id {
		t.Fatalf("cap after sync = %v, want [%s]", got, id)
	}
}

// An uncapped user must stay uncapped: the column has to survive the round trip
// as SQL NULL, not as an empty array. Since Task 4 a non-NULL cap restricts to
// exactly its members even when empty, so a nil-to-empty slip here would lock a
// deliberately unrestricted account out of every provider.
func TestConfigSync_UncappedUserStaysNull(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	seedUser(t, "alice", nil, true, nil)

	env := doExport(t, r)
	found := false
	for _, u := range env.Config.Users {
		if u.Username != "alice" {
			continue
		}
		found = true
		if u.AllowedProviderNames != nil {
			t.Fatalf("exported cap = %v, want nil for an uncapped user", *u.AllowedProviderNames)
		}
	}
	if !found {
		t.Fatal("export did not carry user alice")
	}

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []string
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT allowed_providers FROM users WHERE username = 'alice'`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != nil {
		t.Fatalf("cap after sync = %v, want NULL", got)
	}
}

// A capped user whose providers do not resolve here must REFUSE the import.
// Unlike a virtual key it cannot be skipped: the declarative replace would
// delete the user, and writing NULL would promote them to unrestricted.
// Refusing must also be total, so the surviving bystander below is the real
// assertion: the users DELETE runs before the per-user loop, so a partial
// refusal would take an unrelated account with it.
func TestConfigSync_RefusesUnresolvableUserCap(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	// Seeded after the export so bob is absent from the envelope and therefore
	// in scope of the declarative delete this import must roll back.
	seedUser(t, "bob", nil, true, nil)

	ghost := []string{"provider-that-does-not-exist-here"}
	env.Config.Users = []ExportUser{{
		Username:             "alice",
		PasswordHash:         "$argon2id$v=19$m=65536,t=3,p=4$c2VlZHNlZWRzZWVk$c2VlZHNlZWRzZWVkc2VlZHNlZWQ",
		Role:                 "user",
		Grants:               []string{},
		Enabled:              true,
		AllowedProviderNames: &ghost,
	}}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	// Pin the reason, so an unrelated 400 (the provider-wipe rail, a schema
	// mismatch) cannot masquerade as this refusal.
	if body := rec.Body.String(); !strings.Contains(body, errUnresolvableUserProviders.Error()) {
		t.Fatalf("refusal body = %q, want it to carry %q", body, errUnresolvableUserProviders.Error())
	}
	names := listUsernames(t)
	if names["alice"] {
		t.Fatal("refused import still wrote the user")
	}
	if !names["bob"] {
		t.Fatal("refused import deleted a bystander user; the rollback was partial")
	}
}
