package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
)

// The import path applies the in-flight ceiling bound the admin API applies
// (Strix 2026-09-01 vuln-0004): a value the runtime would read as "no
// ceiling" (zero or less), or a typo past the ceiling, is refused with a 400
// and nothing is written, instead of being stored, exported and shipped to
// every member on the next sync. In-range values and null still apply.
func TestConfigSync_RefusesOutOfRangeMaxInFlight(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *int
		want  int
	}{
		{"null is no ceiling", nil, http.StatusOK},
		{"floor", new(1), http.StatusOK},
		{"ceiling", new(provider.MaxInFlightCeiling), http.StatusOK},
		{"zero reads as no ceiling at runtime", new(0), http.StatusBadRequest},
		{"negative", new(-5), http.StatusBadRequest},
		{"past the ceiling", new(provider.MaxInFlightCeiling + 1), http.StatusBadRequest},
		{"absurd", new(999999999), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigTables(t)
			r := newConfigSyncRouter(t, configSyncMasterKey)
			seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
			env := doExport(t, r)
			if len(env.Config.Providers) != 1 {
				t.Fatalf("export carries %d providers, want 1", len(env.Config.Providers))
			}
			env.Config.Providers[0].MaxInFlight = tc.value

			rec := doImport(t, r, env, "")
			if rec.Code != tc.want {
				t.Fatalf("import status = %d, want %d; body %s", rec.Code, tc.want, rec.Body.String())
			}
			var stored *int
			if err := apiTestDB.Pool().QueryRow(t.Context(), `SELECT max_in_flight FROM providers WHERE name = 'prov-a'`).Scan(&stored); err != nil {
				t.Fatalf("read row: %v", err)
			}
			if tc.want == http.StatusOK {
				if (stored == nil) != (tc.value == nil) || (stored != nil && *stored != *tc.value) {
					t.Fatalf("stored max_in_flight = %v, want %v", stored, tc.value)
				}
				return
			}
			if stored != nil {
				t.Fatalf("a refused envelope wrote max_in_flight = %d", *stored)
			}
			if body := rec.Body.String(); !strings.Contains(body, errInvalidSyncedProvider.Error()) || !strings.Contains(body, "prov-a") {
				t.Fatalf("refusal body = %q, want it to carry %q and the provider's name", body, errInvalidSyncedProvider.Error())
			}
		})
	}
}

// The import's provider validation is the admin API's rule, not a copy of
// it: the same function decides both, so the two cannot drift apart.
func TestValidateSyncedProvider_IsTheInteractiveRule(t *testing.T) {
	for _, v := range []*int{nil, new(1), new(10000), new(0), new(-1), new(10001)} {
		p := ExportProvider{Name: "p", BaseURL: "https://p.example.test/v1", MaxInFlight: v}
		synced := validateSyncedProvider(p)
		interactive := provider.ValidateMaxInFlight(v)
		if (synced != nil) != (interactive != nil) {
			t.Fatalf("value %v: import path (%v) and admin API (%v) disagree", v, synced, interactive)
		}
		if synced != nil && !errors.Is(synced, errInvalidSyncedProvider) {
			t.Fatalf("error %v does not wrap errInvalidSyncedProvider", synced)
		}
	}
	long := ExportProvider{Name: "p", BaseURL: "https://" + strings.Repeat("a", maxProviderURLLen) + ".example.test/v1"}
	if err := validateSyncedProvider(long); err == nil || !errors.Is(err, errInvalidSyncedProvider) {
		t.Fatalf("an over-long base_url was not refused: %v", err)
	}
}

// A base_url the admin API refuses (a private address) is a 400 on import
// too, not a 500: the envelope carries what the dashboard would reject.
func TestConfigSync_RefusesPrivateBaseURLAsBadRequest(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouterWithURLCheck(t, configSyncMasterKey)
	seedProvider(t, "prov-a", "sk-secret", configSyncMasterKey)
	env := doExport(t, r)
	env.Config.Providers[0].BaseURL = "http://192.168.1.141:5001/v1"
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "invalid base_url") {
		t.Fatalf("refusal body = %q, want the base_url reason", body)
	}
}

// newConfigSyncRouterWithURLCheck is newConfigSyncRouter with the same
// base_url guard the admin API applies (config.ValidateProviderURL), for a
// test about what the import refuses on a URL's account.
func newConfigSyncRouterWithURLCheck(t *testing.T, masterKey string) chi.Router {
	t.Helper()
	h := NewConfigSyncHandler(apiTestDB, settings.NewRepository(apiTestDB.Pool()), masterKey, "v-test", nil, (&config.Config{}).ValidateProviderURL)
	r := chi.NewRouter()
	h.Register(r)
	return r
}
