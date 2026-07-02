package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// seedUser inserts a user row directly and returns its username.
func seedUser(t *testing.T, username string, email *string, enabled bool, grants []string) {
	t.Helper()
	hash, err := user.HashPassword("seed-password-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if grants == nil {
		grants = []string{}
	}
	_, err = apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO users (username, display_name, email, password_hash, role, grants, enabled)
		VALUES ($1, $2, $3, $4, 'user', $5, $6)`,
		username, "Seed "+username, email, hash, grants, enabled)
	if err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
}

func cleanUsersTable(t *testing.T) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(), `TRUNCATE users CASCADE`); err != nil {
		t.Fatalf("truncate users: %v", err)
	}
}

func listUsernames(t *testing.T) map[string]bool {
	t.Helper()
	rows, err := apiTestDB.Pool().Query(context.Background(), `SELECT username FROM users`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[n] = true
	}
	return out
}

// Users ride the export/import round trip: added, updated, and removed
// declaratively, with the password hash carried verbatim.
func TestConfigSync_UsersRoundTrip(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	// Primary state: one provider (so the empty-config guard passes) + two users.
	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	email := "worker@example.com"
	seedUser(t, "keep", &email, true, []string{"chat"})
	seedUser(t, "gone-on-primary", nil, true, nil)
	env := doExport(t, r)
	if len(env.Config.Users) != 2 {
		t.Fatalf("export carried %d users, want 2", len(env.Config.Users))
	}

	// Member state: "keep" exists with old data, "member-only" must be removed.
	cleanUsersTable(t)
	seedUser(t, "keep", nil, false, nil)
	seedUser(t, "member-only", nil, true, nil)

	// Drop one user from the envelope so the member sees add/update/remove at once.
	users := env.Config.Users[:0]
	for _, u := range env.Config.Users {
		if u.Username != "gone-on-primary" {
			users = append(users, u)
		}
	}
	env.Config.Users = users

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := listUsernames(t)
	if !got["keep"] || got["member-only"] || len(got) != 1 {
		t.Fatalf("users after import = %v, want only keep", got)
	}

	// The member's row converged: enabled again, email + grants ported, and the
	// primary's password verifies against the carried hash.
	var enabled bool
	var gotEmail *string
	var grants []string
	var hash string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT enabled, email, grants, password_hash FROM users WHERE username = 'keep'`).
		Scan(&enabled, &gotEmail, &grants, &hash)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || gotEmail == nil || *gotEmail != email || len(grants) != 1 || grants[0] != "chat" {
		t.Errorf("row did not converge: enabled=%v email=%v grants=%v", enabled, gotEmail, grants)
	}
	if ok, _ := user.VerifyPassword("seed-password-1", hash); !ok {
		t.Error("ported hash does not verify the primary's password")
	}
}

// A nil users slice (an envelope from an older primary) leaves the member's
// users untouched, mirroring the failover-groups contract.
func TestConfigSync_NilUsersLeavesMemberAlone(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.Users = nil // model a pre-users primary

	seedUser(t, "local-user", nil, true, nil)
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := listUsernames(t); !got["local-user"] {
		t.Errorf("nil users slice deleted the member's users: %v", got)
	}
}

// An email swapped between two accounts on the primary imports cleanly (the
// blank-then-upsert sequence avoids a transient unique violation).
func TestConfigSync_UsersEmailSwap(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	a, b := "a@example.com", "b@example.com"
	seedUser(t, "user-a", &a, true, nil)
	seedUser(t, "user-b", &b, true, nil)
	env := doExport(t, r)

	// Member has the emails the other way around.
	cleanUsersTable(t)
	seedUser(t, "user-a", &b, true, nil)
	seedUser(t, "user-b", &a, true, nil)

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("email-swap import failed: %d, body %s", rec.Code, rec.Body.String())
	}
	var gotA string
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT email FROM users WHERE username = 'user-a'`).Scan(&gotA); err != nil {
		t.Fatal(err)
	}
	if gotA != a {
		t.Errorf("user-a email = %q, want %q", gotA, a)
	}
}

// A user edit must move the config-version hash, or Front Desk's auto-sync
// would never notice it. Logins must NOT move it (last_login is not exported).
func TestConfigSync_VersionTracksUsers(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)

	before := doVersion(t, r)
	seedUser(t, "vuser", nil, true, nil)
	after := doVersion(t, r)
	if before == after {
		t.Error("version hash did not change when a user was added")
	}
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE users SET last_login_at = NOW() WHERE username = 'vuser'`); err != nil {
		t.Fatal(err)
	}
	if got := doVersion(t, r); got != after {
		t.Error("a login (last_login_at) moved the version hash; it must not")
	}
}

// The dry-run diff reports users without writing them.
func TestConfigSync_UsersDiffDryRun(t *testing.T) {
	cleanConfigTables(t)
	cleanUsersTable(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	seedUser(t, "new-user", nil, true, nil)
	env := doExport(t, r)

	cleanUsersTable(t)
	seedUser(t, "old-user", nil, true, nil)

	rec := doImport(t, r, env, "?dryRun=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run status = %d", rec.Code)
	}
	var resp importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Diff.Users.Added) != 1 || resp.Diff.Users.Added[0] != "new-user" {
		t.Errorf("diff added = %v", resp.Diff.Users.Added)
	}
	if len(resp.Diff.Users.Removed) != 1 || resp.Diff.Users.Removed[0] != "old-user" {
		t.Errorf("diff removed = %v", resp.Diff.Users.Removed)
	}
	if got := listUsernames(t); !got["old-user"] || got["new-user"] {
		t.Errorf("dry run wrote users: %v", got)
	}
}
