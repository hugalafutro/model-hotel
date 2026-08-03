package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A key restricted to providers that have since been deleted must NOT come back
// unrestricted from a config sync. Before this fix the export dropped every
// unresolvable UUID, producing an empty name list that the import read as "no
// restriction" and wrote as NULL, silently widening the key on every member.
func TestConfigSync_StaleAllowListDoesNotWidenKey(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)

	stale := uuid.New().String()
	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers)
		VALUES ('restricted', 'hash-restricted', 'sk-...re', $1)`, []string{stale}); err != nil {
		t.Fatalf("seed vk: %v", err)
	}

	env := doExport(t, r)
	found := false
	for _, v := range env.Config.VirtualKeys {
		if v.KeyHash != "hash-restricted" {
			continue
		}
		found = true
		if v.AllowedProviderNames == nil {
			t.Fatal("export lost the fact that the key is restricted")
		}
		if len(*v.AllowedProviderNames) != 0 {
			t.Fatalf("AllowedProviderNames = %v, want present but empty", *v.AllowedProviderNames)
		}
	}
	// Without this the export assertions above are vacuous: an export that
	// dropped restricted keys entirely would iterate zero matches and pass.
	if !found {
		t.Fatal("restricted key missing from the export envelope")
	}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	// The row must still be here: correct behaviour skips the UPDATE, and the
	// key hash is in the envelope so the declarative delete spares it. Treating a
	// Scan error as "not escalated" would let a missing row, a typo in the query,
	// or a dead pool make this regression test pass green forever.
	var allowed []string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT allowed_providers FROM virtual_keys WHERE key_hash = 'hash-restricted'`).Scan(&allowed)
	if err != nil {
		t.Fatalf("reading back the restricted key: %v", err)
	}
	if allowed == nil {
		t.Fatal("ESCALATION: restricted key became unrestricted after a config sync")
	}
}

// An unrestricted key must still round-trip as unrestricted: the pointer has to
// stay nil on the wire, or every open key would import as deny-all.
func TestConfigSync_UnrestrictedKeyStaysUnrestricted(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)

	if _, err := apiTestDB.Pool().Exec(context.Background(), `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers)
		VALUES ('open', 'hash-open', 'sk-...op', NULL)`); err != nil {
		t.Fatalf("seed vk: %v", err)
	}

	env := doExport(t, r)
	found := false
	for _, v := range env.Config.VirtualKeys {
		if v.KeyHash != "hash-open" {
			continue
		}
		found = true
		if v.AllowedProviderNames != nil {
			t.Fatalf("unrestricted key exported as restricted: %v", *v.AllowedProviderNames)
		}
	}
	if !found {
		t.Fatal("unrestricted key missing from the export envelope")
	}
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
}

// upsertVirtualKeys hands a plain []string to pgx as the allowed_providers
// parameter and relies on a nil slice encoding as SQL NULL. That assumption is
// load-bearing in the widest possible way: a non-NULL list restricts to exactly
// its members (effectiveAllowedProviders in internal/proxy), so if pgx ever
// encoded nil as '{}' every config-synced unrestricted key would turn deny-all
// on the next sync. Assert the stored column, not a round-trip, because a
// round-trip through the same encoder would agree with itself either way.
func TestConfigSync_ImportOfUnrestrictedKeyStoresNull(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			Providers: []ExportProvider{
				{Name: "prov-a", BaseURL: "https://p", Enabled: true, AutodiscoveryEnabled: true},
			},
			// No AllowedProviderNames at all: the wire shape an older primary
			// also produces for an unrestricted key.
			VirtualKeys: []ExportVK{
				{Name: "vk-open", KeyHash: "hash-open", KeyPreview: "sk-...op"},
			},
		},
	}
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	var isNull bool
	if err := apiTestDB.Pool().QueryRow(ctx,
		`SELECT allowed_providers IS NULL FROM virtual_keys WHERE key_hash = 'hash-open'`).Scan(&isNull); err != nil {
		t.Fatalf("unrestricted key was not imported: %v", err)
	}
	if !isNull {
		t.Fatal("unrestricted key stored a non-NULL allowed_providers: it is now deny-all, not open")
	}
}
