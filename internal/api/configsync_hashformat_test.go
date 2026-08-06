package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// A password hash arriving over the wire is the one credential field this
// member did not compute itself. Login already fails closed on a malformed
// hash, so this is not an authentication bypass; the point is that an unusable
// hash must not be written at all, since it would otherwise surface much later
// as an account that silently cannot log in.
func TestConfigSync_RefusesMalformedPasswordHash(t *testing.T) {
	cases := []struct {
		name string
		hash string
	}{
		{"plaintext", "hunter2"},
		{"bcrypt", "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"},
		{"empty", ""},
		{"truncated argon2id", "$argon2id$v=19$m=65536,t=3,p=4$c2VlZHNlZWRzZWVk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigTables(t)
			r := newConfigSyncRouter(t, configSyncMasterKey)

			seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
			env := doExport(t, r)
			// Seeded after the export so bob is absent from the envelope and
			// therefore in scope of the declarative delete this import must
			// roll back.
			seedUser(t, "bob", nil, true, nil)
			env.Config.Users = []ExportUser{{
				Username:     "imported",
				PasswordHash: tc.hash,
				Role:         "user",
				Grants:       []string{},
				Enabled:      true,
			}}

			rec := doImport(t, r, env, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("import status = %d, want %d; body %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			// Pin the reason, so an unrelated 400 (the provider-wipe rail, a
			// schema mismatch) cannot masquerade as this refusal.
			if body := rec.Body.String(); !strings.Contains(body, errInvalidSyncedPasswordHash.Error()) {
				t.Fatalf("refusal body = %q, want it to carry %q", body, errInvalidSyncedPasswordHash.Error())
			}
			names := listUsernames(t)
			if names["imported"] {
				t.Fatal("refused import still wrote the user")
			}
			if !names["bob"] {
				t.Fatal("refused import deleted a bystander user; the rollback was partial")
			}
		})
	}
}

// The ordinary path must keep working: a genuine argon2id hash imports and the
// account lands. Without this, a validator that was too strict would quietly
// break every fleet sync carrying users.
func TestConfigSync_AcceptsGenuinePasswordHash(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.Users = []ExportUser{{
		Username:     "imported",
		PasswordHash: "$argon2id$v=19$m=65536,t=3,p=4$c2VlZHNlZWRzZWVk$c2VlZHNlZWRzZWVkc2VlZHNlZWQ",
		Role:         "user",
		Grants:       []string{},
		Enabled:      true,
	}}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if names := listUsernames(t); !names["imported"] {
		t.Fatal("valid user was not imported")
	}
}

// Import refuses an envelope carrying an unparseable hash, which would freeze
// convergence for the whole fleet over one unusable account. The primary
// therefore flags its own corruption at export time, where it can be fixed,
// instead of leaving every member to reject the envelope with no clue which
// account is at fault. Export itself must still succeed: withholding the config
// would wedge the fleet for the same bad reason.
func TestConfigSync_ExportFlagsMalformedStoredHash(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	seedUser(t, "corrupted", nil, true, nil)

	// Only a direct write or DB corruption can produce this state, which is
	// exactly the case the export-side check exists for.
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE users SET password_hash = 'not-a-hash' WHERE username = 'corrupted'`); err != nil {
		t.Fatalf("seed malformed hash: %v", err)
	}

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	env := doExport(t, r)

	found := false
	for _, u := range env.Config.Users {
		if u.Username == "corrupted" {
			found = true
		}
	}
	if !found {
		t.Error("export dropped the user; it must still serve config so the fleet is not wedged")
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type == "configsync.malformed_password_hash" {
				if name, _ := ev.Metadata["username"].(string); name != "corrupted" {
					t.Errorf("event names %q, want the corrupted account", name)
				}
				return
			}
		case <-deadline:
			t.Fatal("export did not raise configsync.malformed_password_hash")
		}
	}
}
