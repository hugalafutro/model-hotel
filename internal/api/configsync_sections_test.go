package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
)

// doVersionSections reads GET /config/version and returns the full response:
// the overall hash plus the per-section hashes Front Desk uses to name what
// diverged in the config.auto_synced event.
func doVersionSections(t *testing.T, r chi.Router) (string, map[string]string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/version", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("version status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Version  string            `json:"version"`
		Sections map[string]string `json:"sections"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	return resp.Version, resp.Sections
}

// TestConfigSync_VersionCarriesSectionHashes: the version response names a hash
// per payload section, keyed by the section's JSON field name, so Front Desk can
// tell WHICH part of the config a diverged member differs in without pulling
// either export.
func TestConfigSync_VersionCarriesSectionHashes(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	_, sections := doVersionSections(t, r)

	want := []string{"providers", "virtual_keys", "settings", "failover_groups", "users", "disabled_models"}
	// Tied to the struct, not to a hardcoded count: a field added to
	// ConfigPayload starts riding in the overall hash immediately, and without
	// this guard the sections map would quietly stop explaining divergences in
	// it (here, and in Front Desk's copy of the key list in
	// internal/frontdesk/autosync.go configSections).
	if got := reflect.TypeFor[ConfigPayload]().NumField(); got != len(want) {
		t.Fatalf("ConfigPayload has %d fields but the version endpoint hashes %d sections; add the new section to Version's map, this test, and Front Desk's configSections", got, len(want))
	}
	if len(sections) != len(want) {
		t.Fatalf("sections = %v, want exactly the %d payload sections %v", sections, len(want), want)
	}
	for _, k := range want {
		if sections[k] == "" {
			t.Errorf("section %q missing or empty in %v", k, sections)
		}
	}

	// Stable: an unchanged config reads the same section hashes.
	_, again := doVersionSections(t, r)
	for _, k := range want {
		if again[k] != sections[k] {
			t.Errorf("section %q hash moved without a config change: %q -> %q", k, sections[k], again[k])
		}
	}
}

// TestConfigSync_SectionHashMovesAloneWithItsSection: changing one section moves
// that section's hash and no other, which is exactly the property the auto-sync
// event's diff summary rests on.
func TestConfigSync_SectionHashMovesAloneWithItsSection(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	v1, before := doVersionSections(t, r)

	// A provider change moves providers alone.
	seedProvider(t, "anthropic", "sk-other-value", configSyncMasterKey)
	v2, afterProvider := doVersionSections(t, r)
	if v2 == v1 {
		t.Error("overall hash unchanged after adding a provider")
	}
	if afterProvider["providers"] == before["providers"] {
		t.Error("providers section hash unchanged after adding a provider")
	}
	for _, k := range []string{"virtual_keys", "settings", "failover_groups", "users", "disabled_models"} {
		if afterProvider[k] != before[k] {
			t.Errorf("section %q hash moved on a provider-only change: %q -> %q", k, before[k], afterProvider[k])
		}
	}

	// A virtual-key change moves virtual_keys alone.
	if _, err := apiTestDB.Pool().Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview)
		VALUES ('vk-1', 'hash-1', 'mh-***')`); err != nil {
		t.Fatalf("seed vk: %v", err)
	}
	_, afterVK := doVersionSections(t, r)
	if afterVK["virtual_keys"] == afterProvider["virtual_keys"] {
		t.Error("virtual_keys section hash unchanged after adding a key")
	}
	for _, k := range []string{"providers", "settings", "failover_groups", "users", "disabled_models"} {
		if afterVK[k] != afterProvider[k] {
			t.Errorf("section %q hash moved on a key-only change: %q -> %q", k, afterProvider[k], afterVK[k])
		}
	}
}
