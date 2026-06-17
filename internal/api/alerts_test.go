package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/alert"
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
