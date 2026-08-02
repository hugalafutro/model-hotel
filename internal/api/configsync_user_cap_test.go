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

// The primary deleting the last provider in an account's cap must NOT wedge
// fleet sync. The cap arrives present-but-empty (the primary itself resolves
// nothing), and the member writes an empty array: proxy.effectiveAllowedProviders
// reads a non-nil cap as "exactly these providers" even when empty, so `{}`
// reproduces the primary's deny-everything behaviour, where NULL would mean
// every provider.
//
// This also pins a pgx encoding assumption the branch now depends on, in the
// opposite direction from the nil-encodes-as-NULL test Task 5 added: a non-nil
// EMPTY []string must encode as `{}` and not collapse to NULL. If it collapsed,
// this path would write "unrestricted" and silently re-open the escalation the
// branch exists to close. The assertion is therefore made in SQL against the
// column (IS NOT NULL / cardinality), not by scanning the array back through
// the same codec that wrote it, which would agree with itself either way.
func TestConfigSync_EmptyUserCapImportsAsEmptyArray(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	gone := seedProvider(t, "prov-gone", "sk-secret", configSyncMasterKey)
	seedUser(t, "alice", nil, true, nil)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE users SET allowed_providers = $1 WHERE username = 'alice'`, []string{gone}); err != nil {
		t.Fatalf("set cap: %v", err)
	}
	// The operator action that motivates this case: delete the only provider in
	// alice's cap, leaving a dangling UUID behind (the column has no FK).
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`DELETE FROM providers WHERE id = $1`, gone); err != nil {
		t.Fatalf("delete provider: %v", err)
	}

	env := doExport(t, r)
	found := false
	for _, u := range env.Config.Users {
		if u.Username != "alice" {
			continue
		}
		found = true
		if u.AllowedProviderNames == nil {
			t.Fatal("exported cap = nil, want present-but-empty for a fully dangling cap")
		}
		if len(*u.AllowedProviderNames) != 0 {
			t.Fatalf("exported cap = %v, want empty", *u.AllowedProviderNames)
		}
	}
	if !found {
		t.Fatal("export did not carry user alice")
	}

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200 (an empty cap must not wedge sync); body %s", rec.Code, rec.Body.String())
	}
	var present bool
	var cardinality int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT allowed_providers IS NOT NULL, coalesce(cardinality(allowed_providers), -1)
		   FROM users WHERE username = 'alice'`).Scan(&present, &cardinality); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !present {
		t.Fatal("stored cap is NULL; pgx collapsed an empty slice and the account is now unrestricted")
	}
	if cardinality != 0 {
		t.Fatalf("stored cap cardinality = %d, want 0", cardinality)
	}
}

// A capped user naming providers that do not resolve here must REFUSE the
// import. Unlike the empty-cap case above this is anomalous, not operational:
// providers are replaced declaratively earlier in the same transaction, so a
// legitimate primary's names always resolve. It cannot be skipped either, the
// way a virtual key is: the declarative replace would delete the user, and
// writing NULL would promote them to unrestricted.
//
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
