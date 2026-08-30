package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/settings"
)

// validateSyncedSetting enforces the interactive minimum on a numeric setting
// and deliberately lets a too-large or unparseable one through. See the function
// comment for why the ceiling is not mirrored: it relaxes no enforcement, and
// rejecting it would make an older member refuse a newer primary's whole
// envelope the first time a ceiling is raised.
func TestValidateSyncedSetting_NumericBounds(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{name: "ip burst below minimum denies every client IP", key: "rate_limit_ip_burst", value: "-1", wantErr: true},
		{name: "ip burst zero denies every client IP", key: "rate_limit_ip_burst", value: "0", wantErr: true},
		{name: "global burst below minimum", key: "rate_limit_burst", value: "0", wantErr: true},
		{name: "negative float rps", key: "rate_limit_ip_rps", value: "-0.5", wantErr: true},
		{name: "negative max wait", key: "rate_limit_max_wait_ms", value: "-1", wantErr: true},
		{name: "breaker threshold below minimum", key: "circuit_breaker_threshold", value: "0", wantErr: true},
		// A span below 1 is the one value that changes enforcement in the
		// dangerous direction: it would indict a provider with nothing open.
		{name: "breaker span below minimum", key: "circuit_breaker_span_models", value: "0", wantErr: true},
		{name: "breaker span of one is the escape hatch, not an error", key: "circuit_breaker_span_models", value: "1"},

		{name: "minimum itself is legal", key: "rate_limit_ip_burst", value: "1"},
		{name: "ordinary value", key: "rate_limit_burst", value: "20"},
		{name: "zero is legal where the minimum is zero", key: "rate_limit_rps", value: "0"},
		{name: "global tpm zero means no cap", key: "rate_limit_tpm", value: "0"},
		{name: "above the ceiling is not an enforcement bound", key: "rate_limit_burst", value: "999999"},
		{name: "unparseable int falls back to the default", key: "rate_limit_burst", value: "abc"},
		{name: "unparseable float falls back to the default", key: "rate_limit_ip_rps", value: ""},
		{name: "string-typed setting is untouched", key: "rate_limit_enabled", value: "true"},
		{name: "unknown key passes through", key: "not_a_setting", value: "-9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSyncedSetting(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSyncedSetting(%q, %q) error = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, errInvalidSyncedSettingBound) {
				t.Fatalf("error %v does not wrap errInvalidSyncedSettingBound", err)
			}
		})
	}
}

// The interactive endpoint's minimums must hold on the import path too. The
// limiter floors are the dangerous ones: IPLimiter.getLimiter hands
// rate_limit_ip_burst to rate.NewLimiter with no clamp, so a negative one
// imported from a compromised primary denies every request from every client IP
// on this member — a wider outage than the per-key limits validateSyncedRateLimits
// guards. The whole envelope must be refused and rolled back.
func TestConfigSync_ImportRejectsOutOfRangeSetting(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"ip burst negative", "rate_limit_ip_burst", "-1"},
		{"global burst zero", "rate_limit_burst", "0"},
		{"breaker threshold zero", "circuit_breaker_threshold", "0"},
		{"breaker span zero", "circuit_breaker_span_models", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigTables(t)
			r := newConfigSyncRouter(t, configSyncMasterKey)
			seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
			env := doExport(t, r)
			if env.Config.Settings == nil {
				env.Config.Settings = map[string]string{}
			}
			env.Config.Settings[tc.key] = tc.value

			cleanConfigTables(t) // fresh replica: the whole envelope must fail atomically
			rec := doImport(t, r, env, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("out-of-range %s status = %d, want 400; body %s", tc.key, rec.Code, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(tc.key)) {
				t.Errorf("rejection body should name the setting %q, got %s", tc.key, rec.Body.String())
			}

			ctx := context.Background()
			var providers, poisoned int
			_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM providers`).Scan(&providers)
			if providers != 0 {
				t.Errorf("refused import must roll back: providers = %d, want 0", providers)
			}
			_ = apiTestDB.Pool().QueryRow(ctx,
				`SELECT count(*) FROM settings WHERE key = $1`, tc.key).Scan(&poisoned)
			if poisoned != 0 {
				t.Errorf("out-of-range %s must not be written, count = %d", tc.key, poisoned)
			}
		})
	}
}

// The guard must not break a rolling upgrade: a value this member considers too
// large is still applied, because a newer primary raising a ceiling must not make
// every older member reject the entire envelope over one field.
func TestConfigSync_ImportAcceptsSettingAboveThisMembersCeiling(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	if env.Config.Settings == nil {
		env.Config.Settings = map[string]string{}
	}
	env.Config.Settings["rate_limit_burst"] = "999999" // above this member's max of 10000

	cleanConfigTables(t)
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("above-ceiling import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := settings.NewRepository(apiTestDB.Pool()).
		GetWithDefault(ctx, "rate_limit_burst", ""); got != "999999" {
		t.Fatalf("rate_limit_burst = %q, want it applied", got)
	}
}
