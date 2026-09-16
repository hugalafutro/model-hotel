package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedSyncableSetting(t *testing.T, key, value string) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value); err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
}

func settingsCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM settings WHERE key = ANY($1)`, syncableSettingKeys()).Scan(&n); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	return n
}

// An envelope with no settings map at all onto a member that holds syncable
// settings is refused, like the provider and key rails: applying it would
// reset every operator setting on the member in one push.
func TestConfigSync_RefusesSettingsWipingImport(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	seedSyncableSetting(t, "request_timeout", "45s")
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.Settings = nil

	_, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("settings-wiping import: code=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "delete every syncable setting") {
		t.Errorf("refusal body = %q, want the settings rail's own message", rec.Body.String())
	}
	if got := settingsCount(t); got != 1 {
		t.Errorf("syncable settings after refused import = %d, want the seeded one to survive", got)
	}
}

// The operator-intent side: a primary running on defaults exports
// "settings": {}, and a member holding overrides converges down to defaults
// on it. Only the absent map is refused.
func TestConfigSync_ExplicitEmptySettingsReconcileToDefaults(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	seedSyncableSetting(t, "request_timeout", "45s")
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.Settings = map[string]string{}

	resp, rec := doImportGen(t, r, env, nil)
	if rec.Code != http.StatusOK || !resp.Applied {
		t.Fatalf("empty-settings import: code=%d applied=%v body=%s, want 200 applied", rec.Code, resp.Applied, rec.Body.String())
	}
	if got := settingsCount(t); got != 0 {
		t.Errorf("syncable settings after explicit empty map = %d, want 0", got)
	}
}

// A member with no syncable settings yet has nothing to lose, so the absent
// map is a bootstrap and applies.
func TestConfigSync_NoSettingsMapOntoDefaultMemberApplies(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	env := doExport(t, r)
	env.Config.Settings = nil

	resp, rec := doImportGen(t, r, withExtraProvider(env, "extra"), nil)
	if rec.Code != http.StatusOK || !resp.Applied {
		t.Fatalf("bootstrap import: code=%d applied=%v body=%s, want 200 applied", rec.Code, resp.Applied, rec.Body.String())
	}
	if !providerNames(t)["extra"] {
		t.Error("a settings-less envelope onto a default member should apply")
	}
}

// The rail keys on the wire shape, not on the Go value: an omitted settings
// property and an explicit null both decode to no map and are refused, an
// explicit empty object is the primary's intent and applies.
func TestConfigSync_SettingsRailReadsTheWireShape(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(cfg map[string]any)
		wantCode int
	}{
		{"omitted", func(cfg map[string]any) { delete(cfg, "settings") }, http.StatusBadRequest},
		{"null", func(cfg map[string]any) { cfg["settings"] = nil }, http.StatusBadRequest},
		{"empty object", func(cfg map[string]any) { cfg["settings"] = map[string]any{} }, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigTables(t)
			seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
			seedSyncableSetting(t, "request_timeout", "45s")
			r := newConfigSyncRouter(t, configSyncMasterKey)

			raw, err := json.Marshal(doExport(t, r))
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			var env map[string]any
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			cfg, _ := env["config"].(map[string]any)
			if cfg == nil {
				t.Fatalf("envelope has no config object: %s", raw)
			}
			tc.mutate(cfg)
			body, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal mutated envelope: %v", err)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)))
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d body = %s, want %d", rec.Code, rec.Body.String(), tc.wantCode)
			}
			wantLeft := 1
			if tc.wantCode == http.StatusOK {
				wantLeft = 0
			}
			if got := settingsCount(t); got != wantLeft {
				t.Errorf("syncable settings after import = %d, want %d", got, wantLeft)
			}
		})
	}
}
