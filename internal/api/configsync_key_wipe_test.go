package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestConfigSync_RefusesKeyWipingImport covers the credential half of the
// destructive-wipe rail. The declarative delete removes every key absent from
// the envelope, and import's structural guard only refuses an envelope that is
// empty in providers, keys and settings at once, so an envelope carrying
// providers but omitting virtual_keys used to reach the delete and take every
// credential off the member in one push.
func TestConfigSync_RefusesKeyWipingImport(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview)
		VALUES ('live-key', 'hash-live-key', 'mh-***')`); err != nil {
		t.Fatalf("seed virtual key: %v", err)
	}
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.VirtualKeys = nil

	_, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("key-wiping import: code=%d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "virtual key") {
		t.Errorf("refusal body = %q, want it to name the virtual keys", rec.Body.String())
	}

	var keys int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM virtual_keys`).Scan(&keys); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if keys != 1 {
		t.Errorf("virtual keys after refused import = %d, want the seeded key to survive", keys)
	}
}

// TestConfigSync_EmptyKeyListOntoEmptyMemberApplies is the other side: a member
// with no keys yet is a bootstrap, not a wipe, so the same envelope applies.
func TestConfigSync_EmptyKeyListOntoEmptyMemberApplies(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.VirtualKeys = nil

	resp, rec := doImportGen(t, r, withExtraProvider(env, "extra"), nil)
	if rec.Code != http.StatusOK || !resp.Applied {
		t.Fatalf("bootstrap import: code=%d applied=%v, want 200 applied", rec.Code, resp.Applied)
	}
	if !providerNames(t)["extra"] {
		t.Error("a keyless envelope onto a keyless member should apply")
	}
}
