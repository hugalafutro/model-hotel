package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// validateSyncedRateLimits must accept exactly what the interactive
// validateRateLimits accepts, so the import path and the admin API cannot
// disagree about what a legal limit is. Every case below is run through both
// validators and their verdicts compared, so the parity is checked rather than
// restated by a second hand-maintained table.
func TestValidateSyncedRateLimits(t *testing.T) {
	tests := []struct {
		name    string
		rps     *float64
		burst   *int
		tpm     *int
		wantErr bool
	}{
		{name: "all nil falls back to globals"},
		{name: "zero rps means unlimited", rps: new(0.0)},
		{name: "ordinary limits", rps: new(5.0), burst: new(10), tpm: new(60000)},
		{name: "minimum burst and tpm", burst: new(1), tpm: new(1)},
		{name: "negative rps", rps: new(-1.0), wantErr: true},
		{name: "zero burst rejects every request", burst: new(0), wantErr: true},
		{name: "negative burst rejects every request", burst: new(-5), wantErr: true},
		{name: "zero tpm reads as no cap", tpm: new(0), wantErr: true},
		{name: "negative tpm reads as no cap", tpm: new(-1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSyncedRateLimits("virtual key \"k\"", tt.rps, tt.burst, tt.tpm)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSyncedRateLimits() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, errInvalidSyncedRateLimit) {
				t.Fatalf("error %v does not wrap errInvalidSyncedRateLimit", err)
			}

			// Drive the interactive validator over the same input rather than
			// trusting the table above to stay in step with it. Without this the
			// two could drift apart silently: someone relaxing or tightening
			// validateRateLimits would leave this file green while the import path
			// and the admin API started disagreeing about what a legal limit is,
			// which is the whole gap this guard closes.
			interactive := validateRateLimits(tt.rps, tt.burst, tt.tpm, httptest.NewRecorder())
			if (interactive != nil) != (err != nil) {
				t.Fatalf("import path and interactive API disagree: validateSyncedRateLimits() = %v, validateRateLimits() = %v",
					err, interactive)
			}
		})
	}
}

// A compromised primary must not be able to push a virtual key whose TPM the
// TPMLimiter would read as "no cap" (tpm <= 0), which would buy the key
// unmetered token spend past this member's global default. The import is
// refused wholesale, so the transaction rolls back and nothing lands.
func TestConfigSync_RefusesNegativeVirtualKeyTPM(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.VirtualKeys = []ExportVK{{
		Name:         "poisoned",
		KeyHash:      "hash-poisoned",
		KeyPreview:   "sk-...ed",
		RateLimitTPM: new(-1),
	}}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want %d; body %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertNoVirtualKeys(t)
}

// Same guard on the users table: a negative burst alongside a positive RPS makes
// rate.NewLimiter refuse every request, a per-account denial of service.
func TestConfigSync_RefusesNegativeUserBurst(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.Users = []ExportUser{{
		Username:       "victim",
		PasswordHash:   "$argon2id$v=19$m=65536,t=3,p=4$c2VlZHNlZWRzZWVk$c2VlZHNlZWRzZWVkc2VlZHNlZWQ",
		Role:           "user",
		Grants:         []string{},
		Enabled:        true,
		RateLimitRPS:   new(5.0),
		RateLimitBurst: new(-1),
	}}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want %d; body %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if names := listUsernames(t); names["victim"] {
		t.Fatalf("refused import still wrote the user: %v", names)
	}
}

// A legal envelope still applies: the guard must not reject the ordinary path.
func TestConfigSync_AcceptsValidRateLimits(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.VirtualKeys = []ExportVK{{
		Name:           "fine",
		KeyHash:        "hash-fine",
		KeyPreview:     "sk-...ne",
		RateLimitRPS:   new(5.0),
		RateLimitBurst: new(10),
		RateLimitTPM:   new(60000),
	}}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var count int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM virtual_keys WHERE key_hash = 'hash-fine'`).Scan(&count); err != nil {
		t.Fatalf("count virtual keys: %v", err)
	}
	if count != 1 {
		t.Fatalf("virtual key rows = %d, want 1", count)
	}
}

func assertNoVirtualKeys(t *testing.T) {
	t.Helper()
	var count int
	if err := apiTestDB.Pool().QueryRow(context.Background(), `SELECT count(*) FROM virtual_keys`).Scan(&count); err != nil {
		t.Fatalf("count virtual keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("refused import still wrote %d virtual key(s)", count)
	}
}
