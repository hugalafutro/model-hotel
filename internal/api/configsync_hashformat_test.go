package api

import (
	"net/http"
	"testing"
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
			if names := listUsernames(t); names["imported"] {
				t.Fatal("refused import still wrote the user")
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
