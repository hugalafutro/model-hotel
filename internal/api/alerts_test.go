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
	hit := false
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit = true }))
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
	if rec.Code != http.StatusOK || !hit {
		t.Fatalf("targets-only = %d hit=%v", rec.Code, hit)
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
