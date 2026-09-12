package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestConfigSync_RefusesKeyWipingImport covers the credential half of the
// destructive-wipe rail. The declarative delete removes every key absent from
// the envelope, and import's structural guard only refuses an envelope that is
// empty in providers, keys and settings at once, so an envelope whose
// virtual_keys field is missing used to reach the delete and take every
// credential off the member in one push. A nil slice is what an absent field
// decodes to and what it marshals back to (null), so this is that envelope.
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
	if !strings.Contains(rec.Body.String(), "delete every virtual key") {
		t.Errorf("refusal body = %q, want the key rail's own message", rec.Body.String())
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

// TestConfigSync_ExplicitEmptyKeyListReconcilesToZero is the operator-intent
// side of the rail. A primary that really has zero virtual keys exports
// "virtual_keys": [], and a member holding keys has to converge to zero on it,
// or that primary can never sync its fleet. Only the absent field is refused.
func TestConfigSync_ExplicitEmptyKeyListReconcilesToZero(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview)
		VALUES ('live-key', 'hash-live-key', 'mh-***')`); err != nil {
		t.Fatalf("seed virtual key: %v", err)
	}
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.VirtualKeys = []ExportVK{}

	resp, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusOK || !resp.Applied {
		t.Fatalf("explicit empty key list: code=%d applied=%v body=%s, want 200 applied",
			rec.Code, resp.Applied, rec.Body.String())
	}
	var keys int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM virtual_keys`).Scan(&keys); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if keys != 0 {
		t.Errorf("virtual keys after an explicit empty list = %d, want the member reconciled to zero", keys)
	}
}

// TestConfigSync_ExportEmitsEmptyKeyListNotNull pins the other end of that
// contract. The rail can only refuse a null virtual_keys field because a member
// running this code never sends one: the export of a keyless member has to be
// [], or every keyless primary would be refused by its own fleet.
func TestConfigSync_ExportEmitsEmptyKeyListNotNull(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	// doExport decodes the wire bytes, so a non-nil slice here means the wire
	// carried [] rather than null.
	env := doExport(t, r)
	if env.Config.VirtualKeys == nil {
		t.Fatal("export of a keyless member sent virtual_keys: null, which the import rail refuses")
	}
	body, err := json.Marshal(env.Config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if !strings.Contains(string(body), `"virtual_keys":[]`) {
		t.Errorf("exported config = %s, want it to carry virtual_keys as []", body)
	}
}

// TestConfigSync_RefusesImportThatLeavesNoKeys covers the longer road to the
// same wipe: the envelope carries a key, so the pre-reconcile rail passes, but
// the key names a provider restriction that does not resolve on this member, so
// the upsert skips it. The declarative delete still clears the member's own key
// and the member ends the transaction with no credentials at all.
func TestConfigSync_RefusesImportThatLeavesNoKeys(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview)
		VALUES ('live-key', 'hash-live-key', 'mh-***')`); err != nil {
		t.Fatalf("seed virtual key: %v", err)
	}
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	unknownProvider := []string{"a-provider-this-member-does-not-have"}
	env.Config.VirtualKeys = []ExportVK{{
		Name: "incoming", KeyHash: "hash-incoming", KeyPreview: "mh-***",
		AllowedProviderNames: &unknownProvider,
	}}

	_, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import that leaves no keys: code=%d, want 400", rec.Code)
	}
	// Named, not just counted: an ingest-side rejection of the unknown provider
	// restriction would also be a rolled-back 400, and would never reach the
	// delete this rail exists to catch.
	if !strings.Contains(rec.Body.String(), "delete every virtual key") {
		t.Fatalf("refusal body = %q, want the key rail's own message", rec.Body.String())
	}

	names := map[string]bool{}
	rows, err := apiTestDB.Pool().Query(context.Background(), `SELECT name FROM virtual_keys`)
	if err != nil {
		t.Fatalf("query keys: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan key: %v", err)
		}
		names[n] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate keys: %v", err)
	}
	if !names["live-key"] {
		t.Error("the refused import must roll back, leaving the member's own key")
	}
	if names["incoming"] {
		t.Error("the skipped key must not have landed")
	}
}

// TestConfigSync_RefusesProviderWipingImport is the provider half of the same
// rail, held to the same standard: the refusal has to be the rail's own, and the
// member's provider has to survive the rollback.
func TestConfigSync_RefusesProviderWipingImport(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.Providers = nil
	// Settings keep the envelope past import's structural guard, which refuses
	// one that is empty in providers, keys and settings together.
	env.Config.Settings = map[string]string{"request_timeout": "1m0s"}

	_, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("provider-wiping import: code=%d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "delete every provider") {
		t.Errorf("refusal body = %q, want the provider rail's own message", rec.Body.String())
	}
	if !providerNames(t)["openai"] {
		t.Error("the refused import must roll back, leaving the member's provider")
	}
}
