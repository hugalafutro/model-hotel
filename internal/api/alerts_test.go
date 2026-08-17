package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/user"
)

func TestGetAlertEvents(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.GetAlertEvents(
		rec,
		httptest.NewRequest(http.MethodGet, "/alert/events", http.NoBody),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []alert.EventDef
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(alert.Catalog()) {
		t.Fatalf("got %d events, want %d", len(got), len(alert.Catalog()))
	}
	// Spot-check a known entry is present and well-formed.
	var foundOpen bool
	for _, e := range got {
		if e.Type == "circuit_breaker.open" {
			foundOpen = true
			if e.Category == "" || e.Severity == "" {
				t.Errorf("entry missing fields: %+v", e)
			}
		}
	}
	if !foundOpen {
		t.Error("circuit_breaker.open missing from catalog response")
	}
}

func TestGetAlertStatusUnconfigured(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: secretTestMasterKey},
		settingsRepo: &mockSettingsStore{}, // no apprise-api URL
	}
	rec := httptest.NewRecorder()
	h.GetAlertStatus(
		rec,
		httptest.NewRequest(http.MethodGet, "/alert/status", http.NoBody),
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var st alert.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Configured {
		t.Errorf("expected not configured, got %+v", st)
	}
}

func TestGetAlertStatusReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	h := &Handler{
		cfg: &config.Config{MasterKey: secretTestMasterKey},
		settingsRepo: &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, def string) string {
				if key == "alert_apprise_api_url" {
					return srv.URL
				}
				return def
			},
		},
	}
	rec := httptest.NewRecorder()
	h.GetAlertStatus(
		rec,
		httptest.NewRequest(http.MethodGet, "/alert/status", http.NoBody),
	)
	var st alert.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !st.Configured || !st.Reachable || !st.Healthy {
		t.Errorf("expected reachable+healthy, got %+v", st)
	}
}

func TestSendAlertTestUnconfigured(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: secretTestMasterKey},
		settingsRepo: &mockSettingsStore{}, // returns defaults: no URL, no target
	}
	rec := httptest.NewRecorder()
	h.SendAlertTest(
		rec,
		httptest.NewRequest(http.MethodPost, "/alert/test", http.NoBody),
	)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("unconfigured test send should fail, status = %d", rec.Code)
	}
}

func TestSendAlertTestDelivers(t *testing.T) {
	// Stand-in apprise-api that accepts the notify POST.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	h := &Handler{
		cfg: &config.Config{MasterKey: secretTestMasterKey},
		settingsRepo: &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, def string) string {
				switch key {
				case "alert_apprise_api_url":
					return srv.URL
				case "alert_apprise_targets":
					return "tgram://tok/chat"
				}
				return def
			},
		},
	}
	rec := httptest.NewRecorder()
	h.SendAlertTest(
		rec,
		httptest.NewRequest(http.MethodPost, "/alert/test", http.NoBody),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("configured test send should succeed, status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp["ok"] {
		t.Errorf("unexpected response: %s", rec.Body.String())
	}
}

// newCapturingAppriseStub is a stand-in apprise-api that accepts GET /status
// and records the "urls" field of every POST /notify body, so a test can
// assert which destination string SendAlertTest actually dispatched to.
func newCapturingAppriseStub(gotURLs *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.WriteHeader(http.StatusOK)
		case "/notify":
			var p struct {
				URLs string `json:"urls"`
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			*gotURLs = p.URLs
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestAlertProbeEndpoint(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer stub.Close()
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{}}
	rec := httptest.NewRecorder()
	h.ProbeAlert(rec, httptest.NewRequest(http.MethodPost, "/alert/probe", strings.NewReader(`{"api_url":"`+stub.URL+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("probe = %d %s", rec.Code, rec.Body.String())
	}
	var st alert.Status
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Healthy {
		t.Errorf("healthy stub -> %+v", st)
	}

	rec = httptest.NewRecorder()
	h.ProbeAlert(rec, httptest.NewRequest(http.MethodPost, "/alert/probe", strings.NewReader(`{"api_url":"http://127.0.0.1:1"}`)))
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Reason != alert.ReasonUnreachable {
		t.Errorf("reason = %q", st.Reason)
	}

	rec = httptest.NewRecorder()
	h.ProbeAlert(rec, httptest.NewRequest(http.MethodPost, "/alert/probe", strings.NewReader(`{"api_url":" "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("blank = %d", rec.Code)
	}
}

func TestSendAlertTestExplicitConfig(t *testing.T) {
	var gotURLs string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var p struct {
			URLs string `json:"urls"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		gotURLs = p.URLs
		if strings.Contains(p.URLs, "bad") {
			w.WriteHeader(http.StatusFailedDependency)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{}}
	rec := httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test",
		strings.NewReader(`{"api_url":"`+stub.URL+`","targets":["ntfys://ntfy.example.com/one"]}`)))
	if rec.Code != http.StatusOK || gotURLs != "ntfys://ntfy.example.com/one" {
		t.Fatalf("explicit = %d urls=%q", rec.Code, gotURLs)
	}

	rec = httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test",
		strings.NewReader(`{"api_url":"`+stub.URL+`","targets":["ntfys://bad.example/x"]}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("424 -> %d", rec.Code)
	}
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Code != alert.ReasonDeliverFailed {
		t.Errorf("code = %q", e.Code)
	}

	// Empty body keeps today's behaviour: saved config (none here) -> 502 not_configured.
	rec = httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test", http.NoBody))
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusBadGateway || e.Code != alert.ReasonNotConfigured {
		t.Errorf("empty = %d %q", rec.Code, e.Code)
	}
}

func TestSendAlertTestTargetsOnlyUsesSavedURL(t *testing.T) {
	var gotURLs string
	stub := newCapturingAppriseStub(&gotURLs)
	defer stub.Close()
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, def string) string {
			if key == "alert_apprise_api_url" {
				return stub.URL
			}
			return def
		},
	}}
	rec := httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test", strings.NewReader(`{"targets":["ntfys://ntfy.example.com/row"]}`)))
	if rec.Code != http.StatusOK || gotURLs != "ntfys://ntfy.example.com/row" {
		t.Fatalf("targets-only = %d urls=%q", rec.Code, gotURLs)
	}
}

// TestSendAlertTestAPIURLOnlyFallsBackToSavedTarget covers the branch where
// the request supplies api_url but no targets: SendAlertTest must decrypt and
// use the saved target rather than sending to nothing.
func TestSendAlertTestAPIURLOnlyFallsBackToSavedTarget(t *testing.T) {
	var gotURLs string
	stub := newCapturingAppriseStub(&gotURLs)
	defer stub.Close()

	enc, err := auth.EncryptString("ntfys://ntfy.example.com/saved", secretTestMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, def string) string {
			if key == "alert_apprise_targets" {
				return enc
			}
			return def
		},
	}}
	rec := httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test", strings.NewReader(`{"api_url":"`+stub.URL+`"}`)))
	if rec.Code != http.StatusOK || gotURLs != "ntfys://ntfy.example.com/saved" {
		t.Fatalf("api_url-only = %d urls=%q", rec.Code, gotURLs)
	}
}

// TestSendAlertTestAPIURLOnlyUndecryptableSavedTarget covers the same branch
// when the saved target was encrypted under a different MASTER_KEY: the
// decrypt failure must surface as a coded 502, not a silent empty send.
func TestSendAlertTestAPIURLOnlyUndecryptableSavedTarget(t *testing.T) {
	// The decrypt failure must be caught before any request reaches apprise-api,
	// so the stub only needs to exist; it should never receive a call.
	hit := false
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	bad, err := auth.EncryptString("ntfys://ntfy.example.com/x", "another-master-key")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, def string) string {
			if key == "alert_apprise_targets" {
				return bad
			}
			return def
		},
	}}
	rec := httptest.NewRecorder()
	h.SendAlertTest(rec, httptest.NewRequest(http.MethodPost, "/alert/test", strings.NewReader(`{"api_url":"`+stub.URL+`"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("undecryptable = %d %s", rec.Code, rec.Body.String())
	}
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Code != alert.ReasonUndecryptable {
		t.Errorf("code = %q", e.Code)
	}
	if hit {
		t.Error("apprise-api should not be called when the saved target can't be decrypted")
	}
}

func TestGetAlertTargets(t *testing.T) {
	enc, err := auth.EncryptString("ntfys://ntfy.example.com/a; tgram://tok/chat; ntfys://ntfy.example.com/a", secretTestMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	store := &mockSettingsStore{getWithDefaultFn: func(_ context.Context, key, def string) string {
		if key == "alert_apprise_targets" {
			return enc
		}
		return def
	}}
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: store}
	rec := httptest.NewRecorder()
	h.GetAlertTargets(rec, httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody))
	var out struct {
		Targets []string `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("targets = %d %v", rec.Code, err)
	}
	if len(out.Targets) != 2 || out.Targets[1] != "tgram://tok/chat" {
		t.Errorf("targets = %q (dedupe + order)", out.Targets)
	}

	empty := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: &mockSettingsStore{}}
	rec = httptest.NewRecorder()
	empty.GetAlertTargets(rec, httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody))
	if strings.TrimSpace(rec.Body.String()) != `{"targets":[]}` {
		t.Errorf("empty = %s", rec.Body.String())
	}

	bad, _ := auth.EncryptString("x://y", "another-master-key")
	badStore := &mockSettingsStore{getWithDefaultFn: func(_ context.Context, key, def string) string {
		if key == "alert_apprise_targets" {
			return bad
		}
		return def
	}}
	h = &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: badStore}
	rec = httptest.NewRecorder()
	h.GetAlertTargets(rec, httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "undecryptable") {
		t.Errorf("undecryptable = %d %s", rec.Code, rec.Body.String())
	}
}

// The decrypted destination list is the only alert read that returns a
// credential, so a read-only demo (where DEMO_SHOW_TOKEN hands every visitor
// the admin token) must not serve it. readOnlyGuard passes GETs through by
// design, so the guard lives in the handler and this test is what keeps it
// there: without it a demo instance answers with the operator's bot tokens.
func TestGetAlertTargetsHiddenInReadOnlyDemo(t *testing.T) {
	enc, err := auth.EncryptString("tgram://tok/chat", secretTestMasterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	store := &mockSettingsStore{getWithDefaultFn: func(context.Context, string, string) string {
		return enc
	}}
	h := &Handler{
		cfg:          &config.Config{MasterKey: secretTestMasterKey, DemoReadOnly: true},
		settingsRepo: store,
	}

	rec := httptest.NewRecorder()
	h.GetAlertTargets(rec, httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if strings.Contains(rec.Body.String(), "tgram://") {
		t.Errorf("response leaked the decrypted target: %s", rec.Body.String())
	}

	// The same handler on a normal instance still serves the list, so the guard
	// is demo-scoped rather than a blanket refusal.
	h.cfg.DemoReadOnly = false
	rec = httptest.NewRecorder()
	h.GetAlertTargets(rec, httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tgram://tok/chat") {
		t.Errorf("normal instance = %d %s", rec.Code, rec.Body.String())
	}
}

// The alert routes sit inside Register's requireAdmin group. Nothing else pins
// that, and /alert/targets hands back decrypted credentials, so a refactor that
// moved the group up to the usage-grant section would quietly widen it to every
// authenticated account with a green suite. This walks the real guard.
func TestAlertTargetsRequiresAdmin(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: secretTestMasterKey},
		settingsRepo: &mockSettingsStore{},
	}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		h.RegisterAlerts(r)
	})

	for _, tc := range []struct {
		name string
		id   *user.Identity
		want int
	}{
		{"admin", user.AdminIdentity(), http.StatusOK},
		{"usage grant", &user.Identity{Role: user.RoleUser, Grants: []string{string(user.GrantUsage)}}, http.StatusForbidden},
		{"plain user", &user.Identity{Role: user.RoleUser}, http.StatusForbidden},
		{"unauthenticated", nil, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/alert/targets", http.NoBody)
			req = req.WithContext(user.WithIdentity(req.Context(), tc.id))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// An explicit empty api_url is a real value, not an absent one: it overrides
// the stored address, so the test reports "not configured" rather than quietly
// delivering through whatever happens to be saved.
func TestSendAlertTestExplicitEmptyAPIURLOverridesSaved(t *testing.T) {
	store := &mockSettingsStore{getWithDefaultFn: func(_ context.Context, key, def string) string {
		if key == alert.KeyAPIBaseURL {
			return "http://apprise:8000"
		}
		return def
	}}
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}, settingsRepo: store}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alert/test",
		strings.NewReader(`{"api_url":"","targets":["ntfys://n.example.com/t"]}`))
	h.SendAlertTest(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), alert.ReasonNotConfigured) {
		t.Errorf("code = %s, want %s", rec.Body.String(), alert.ReasonNotConfigured)
	}
}

func TestRegisterAlertsRoutes(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	h.RegisterAlerts(r)
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/alert/events"},
		{http.MethodGet, "/alert/status"},
		{http.MethodPost, "/alert/probe"},
		{http.MethodPost, "/alert/test"},
		{http.MethodGet, "/alert/targets"},
	} {
		if !r.Match(chi.NewRouteContext(), tc.method, tc.path) {
			t.Errorf("route not registered: %s %s", tc.method, tc.path)
		}
	}
}
