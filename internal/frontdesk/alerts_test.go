package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
)

// TestCatalogTypesAreEmitted enforces fdCatalog's documented invariant: every
// alertable Type must correspond to an event the package actually publishes, so
// the operator never ticks a checkbox that can never fire. It scans the package's
// non-test Go sources (excluding alerts.go, which only declares the catalog) for a
// quoted literal of each Type. A dead entry (declared but never emitted) fails here.
func TestCatalogTypesAreEmitted(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if name == "alerts.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	haystack := src.String()
	for _, def := range fdCatalog {
		if !strings.Contains(haystack, `"`+def.Type+`"`) {
			t.Errorf("catalog event %q is never emitted in the package; remove it or wire the emit", def.Type)
		}
	}
}

// TestMigrationSeedMatchesCatalogDefaults guards the one hand-maintained pairing:
// migration 007 seeds alert_events with a literal CSV that must equal the DefaultOn
// set of fdCatalog. If someone flips a DefaultOn flag without updating the SQL (or
// vice versa) a fresh install's picker would disagree with the catalog.
func TestMigrationSeedMatchesCatalogDefaults(t *testing.T) {
	store := newTestStore(t)
	set, err := store.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := alert.DefaultEnabledCSVFor(fdCatalog)
	if set.AlertEvents != want {
		t.Errorf("seeded alert_events = %q, want catalog defaults %q", set.AlertEvents, want)
	}
	if want == "" {
		t.Error("fdCatalog has no DefaultOn events; the seed/picker would be empty")
	}
}

func TestAlertConfigProviderDecrypts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	enc, err := auth.EncryptString("tgram://tok/chat", testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set.AlertEnabled = true
	set.AlertAppriseAPIURL = "http://apprise:8000"
	set.AlertAppriseTargets = enc
	set.AlertEvents = "health.down,health.up"
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatal(err)
	}

	p := alertConfigProvider{store: store, masterKey: testMasterKey}
	cfg, err := p.AlertConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.APIBaseURL != "http://apprise:8000" {
		t.Errorf("config = %+v", cfg)
	}
	if cfg.Targets != "tgram://tok/chat" {
		t.Errorf("decrypted target = %q", cfg.Targets)
	}
	if !cfg.Events["health.down"] || !cfg.Events["health.up"] || cfg.Events["config.synced"] {
		t.Errorf("events = %v", cfg.Events)
	}

	// APIBaseURL must not require decrypting the target.
	base, err := p.APIBaseURL(ctx)
	if err != nil || base != "http://apprise:8000" {
		t.Errorf("APIBaseURL = %q, err = %v", base, err)
	}
}

// TestSettingsTargetMaskRoundTrip exercises the HTTP secret boundary: a new target
// is encrypted at rest and never echoed raw, a masked re-submission preserves it,
// and a blank clears it.
func TestSettingsTargetMaskRoundTrip(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	get := func() Settings {
		rec := do(t, srv, http.MethodGet, "/api/settings", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET settings = %d", rec.Code)
		}
		var s Settings
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	put := func(s Settings) {
		b, _ := json.Marshal(s)
		if rec := do(t, srv, http.MethodPut, "/api/settings", string(b), true); rec.Code != http.StatusOK {
			t.Fatalf("PUT settings = %d (%s)", rec.Code, rec.Body.String())
		}
	}
	storedTarget := func() string {
		set, err := store.GetSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return set.AlertAppriseTargets
	}

	// Set a new target.
	s := get()
	s.AlertEnabled = true
	s.AlertAppriseAPIURL = "http://apprise:8000"
	s.AlertAppriseTargets = "tgram://tok/chat"
	put(s)

	// Stored value is encrypted and decrypts to the plaintext.
	if raw := storedTarget(); !auth.IsEncryptedString(raw) {
		t.Errorf("stored target not encrypted: %q", raw)
	}
	if got, _ := auth.DecryptString(storedTarget(), testMasterKey); got != "tgram://tok/chat" {
		t.Errorf("stored target decrypts to %q", got)
	}
	// GET masks it (never the ciphertext or the plaintext).
	if m := get().AlertAppriseTargets; m != alertMaskValue {
		t.Errorf("GET target = %q, want mask", m)
	}

	// Re-submitting the mask preserves the stored secret.
	s2 := get() // target == mask
	s2.AlertAppriseAPIURL = "http://apprise:9000"
	put(s2)
	if got, _ := auth.DecryptString(storedTarget(), testMasterKey); got != "tgram://tok/chat" {
		t.Errorf("after mask resubmit, target = %q, want preserved", got)
	}

	// Blanking the target clears it.
	s3 := get()
	s3.AlertAppriseTargets = ""
	put(s3)
	if raw := storedTarget(); raw != "" {
		t.Errorf("after blank submit, stored target = %q, want cleared", raw)
	}
}

// TestSettingsPartialMergePreservesSecret proves PUT /api/settings is a partial
// merge: a body that omits the alert fields (the polling form, or an older client)
// preserves the stored secret and the rest of the alert config instead of zeroing
// them, while an explicit blank still clears the target on purpose.
func TestSettingsPartialMergePreservesSecret(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	put := func(body string) {
		if rec := do(t, srv, http.MethodPut, "/api/settings", body, true); rec.Code != http.StatusOK {
			t.Fatalf("PUT settings = %d (%s)", rec.Code, rec.Body.String())
		}
	}
	stored := func() Settings {
		set, err := store.GetSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return set
	}

	// Establish a full alert config (encrypts and stores the target).
	put(`{"alert_enabled":true,"alert_apprise_api_url":"http://apprise:8000","alert_apprise_targets":"tgram://tok/chat","alert_events":"health.down"}`)
	if got, _ := auth.DecryptString(stored().AlertAppriseTargets, testMasterKey); got != "tgram://tok/chat" {
		t.Fatalf("setup target = %q", got)
	}

	// A PUT carrying only a polling field must not touch any alert field.
	put(`{"traefik_stale_secs":42}`)
	set := stored()
	if got, _ := auth.DecryptString(set.AlertAppriseTargets, testMasterKey); got != "tgram://tok/chat" {
		t.Errorf("secret erased by partial PUT: %q", got)
	}
	if !set.AlertEnabled {
		t.Error("alert_enabled reverted by partial PUT")
	}
	if set.AlertEvents != "health.down" {
		t.Errorf("alert_events = %q, want preserved", set.AlertEvents)
	}
	if set.TraefikStaleSecs != 42 {
		t.Errorf("traefik_stale_secs = %d, want 42", set.TraefikStaleSecs)
	}

	// An explicit empty target still clears the secret on purpose.
	put(`{"alert_apprise_targets":""}`)
	if raw := stored().AlertAppriseTargets; raw != "" {
		t.Errorf("explicit blank did not clear target: %q", raw)
	}
}

// TestAlertStatusFlagsUndecryptableTarget guards the reachability fix: when a
// target is stored but cannot be decrypted (master key rotated), the status must
// report unhealthy with a reason rather than a falsely green pill.
func TestAlertStatusFlagsUndecryptableTarget(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	// A target encrypted under a different key cannot be decrypted with this server's.
	enc, err := auth.EncryptString("tgram://tok/chat", "a-completely-different-master-key")
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set.AlertEnabled = true
	set.AlertAppriseAPIURL = "http://127.0.0.1:1" // configured, unreachable is fine
	set.AlertAppriseTargets = enc
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatal(err)
	}

	rec := do(t, srv, http.MethodGet, "/api/alert/status", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/alert/status = %d", rec.Code)
	}
	var st alert.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Configured {
		t.Error("status should be configured (URL is set)")
	}
	if st.Healthy {
		t.Error("status should be unhealthy when the stored target cannot be decrypted")
	}
	if !strings.Contains(st.Detail, "decrypt") {
		t.Errorf("detail = %q, want a decrypt reason", st.Detail)
	}
}

// TestAlertStatusFlagsMissingTarget guards the other half of the reachability fix:
// a reachable apprise-api with no notification target still cannot deliver, so it
// must report unhealthy with a reason rather than a green pill.
func TestAlertStatusFlagsMissingTarget(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // apprise-api is up and answers /status
	}))
	defer stub.Close()

	srv, store := newTestServer(t)
	ctx := context.Background()
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set.AlertEnabled = true
	set.AlertAppriseAPIURL = stub.URL
	set.AlertAppriseTargets = "" // reachable, but nowhere to send
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatal(err)
	}

	rec := do(t, srv, http.MethodGet, "/api/alert/status", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/alert/status = %d", rec.Code)
	}
	var st alert.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Reachable {
		t.Fatal("setup: stub should be reachable")
	}
	if st.Healthy {
		t.Error("status should be unhealthy when no target is configured")
	}
	if !strings.Contains(st.Detail, "target") {
		t.Errorf("detail = %q, want a missing-target reason", st.Detail)
	}
}

// TestPutSettingsConcurrentNoClobber fires polling-only and alert-only PUTs at the
// same time. Because each writes only its own fields and the read-merge-write is
// serialized, the final row must still carry both an alert value and a polling
// value: neither category may be wiped by a racing save of the other.
func TestPutSettingsConcurrentNoClobber(t *testing.T) {
	srv, store := newTestServer(t)

	const n = 25
	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := range n {
		go func() {
			defer wg.Done()
			do(t, srv, http.MethodPut, "/api/settings",
				`{"alert_enabled":true,"alert_apprise_api_url":"http://apprise:8000","alert_apprise_targets":"tgram://tok/chat","alert_events":"health.down"}`,
				true)
		}()
		go func(v int) {
			defer wg.Done()
			do(t, srv, http.MethodPut, "/api/settings",
				fmt.Sprintf(`{"traefik_stale_secs":%d}`, 10+v), true)
		}(i)
	}
	wg.Wait()

	set, err := store.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := auth.DecryptString(set.AlertAppriseTargets, testMasterKey); got != "tgram://tok/chat" {
		t.Errorf("alert target wiped under concurrency: %q", got)
	}
	if !set.AlertEnabled || set.AlertEvents != "health.down" {
		t.Errorf("alert config wiped under concurrency: enabled=%v events=%q", set.AlertEnabled, set.AlertEvents)
	}
	if set.TraefikStaleSecs < 10 || set.TraefikStaleSecs > 10+n {
		t.Errorf("traefik_stale_secs = %d, not one of the concurrently written values", set.TraefikStaleSecs)
	}
}

func TestAlertEventsEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/alert/events", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/alert/events = %d", rec.Code)
	}
	var got []alert.EventDef
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(fdCatalog) {
		t.Fatalf("got %d events, want %d", len(got), len(fdCatalog))
	}
	var hasHealthDown bool
	for _, e := range got {
		if e.Type == "health.down" {
			hasHealthDown = e.DefaultOn
		}
	}
	if !hasHealthDown {
		t.Error("health.down missing or not default-on in the served catalog")
	}
}

func TestAlertStatusEndpointUnconfigured(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/alert/status", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/alert/status = %d", rec.Code)
	}
	var st alert.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Configured {
		t.Error("status should report not-configured when no apprise-api URL is set")
	}
}

func TestAlertTestEndpointFailsWithoutConfig(t *testing.T) {
	srv, _ := newTestServer(t)
	// No URL/target configured: TestSend fails and surfaces as 502, not a panic.
	rec := do(t, srv, http.MethodPost, "/api/alert/test", "", true)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /api/alert/test (unconfigured) = %d, want 502", rec.Code)
	}
}
