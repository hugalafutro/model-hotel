package alert

import (
	"context"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

const cfgMasterKey = "config-test-master-key-32-bytes-min!!!!"

// fakeSettings is a map-backed settingsReader. A key absent from the map
// behaves like an unset setting (returns the caller's default).
type fakeSettings struct {
	vals map[string]string
}

func (f fakeSettings) GetWithDefault(_ context.Context, key, def string) string {
	if v, ok := f.vals[key]; ok {
		return v
	}
	return def
}

func (f fakeSettings) GetBool(_ context.Context, key string, def bool) bool {
	if v, ok := f.vals[key]; ok {
		return v == "true"
	}
	return def
}

func TestAlertConfigDefaultsFirstRun(t *testing.T) {
	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{}}, cfgMasterKey)
	cfg, err := p.AlertConfig(context.Background())
	if err != nil {
		t.Fatalf("AlertConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected disabled by default")
	}
	// Unset alert_events seeds from catalog defaults.
	for _, e := range Catalog() {
		if cfg.Events[e.Type] != e.DefaultOn {
			t.Errorf("first-run picker for %q = %v, want %v", e.Type, cfg.Events[e.Type], e.DefaultOn)
		}
	}
}

func TestAlertConfigEmptyEventsMeansNoneFire(t *testing.T) {
	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{"alert_events": ""}}, cfgMasterKey)
	cfg, err := p.AlertConfig(context.Background())
	if err != nil {
		t.Fatalf("AlertConfig: %v", err)
	}
	if len(cfg.Events) != 0 {
		t.Errorf("explicitly-empty alert_events should select nothing, got %v", cfg.Events)
	}
}

func TestAlertConfigDecryptsTarget(t *testing.T) {
	enc, err := auth.EncryptString("tgram://tok/chat", cfgMasterKey)
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{
		"alert_enabled":         "true",
		"alert_apprise_api_url": "http://apprise:8000",
		"alert_apprise_targets": enc,
		"alert_events":          "circuit_breaker.open",
	}}, cfgMasterKey)

	cfg, err := p.AlertConfig(context.Background())
	if err != nil {
		t.Fatalf("AlertConfig: %v", err)
	}
	if !cfg.Enabled || cfg.APIBaseURL != "http://apprise:8000" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if cfg.Targets != "tgram://tok/chat" {
		t.Errorf("target decrypt = %q", cfg.Targets)
	}
	if !cfg.Events["circuit_breaker.open"] || len(cfg.Events) != 1 {
		t.Errorf("events = %v", cfg.Events)
	}
}

func TestAlertConfigPlaintextTargetPassthrough(t *testing.T) {
	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{
		"alert_apprise_targets": "tgram://plain",
	}}, cfgMasterKey)
	cfg, err := p.AlertConfig(context.Background())
	if err != nil {
		t.Fatalf("AlertConfig: %v", err)
	}
	if cfg.Targets != "tgram://plain" {
		t.Errorf("plaintext passthrough = %q", cfg.Targets)
	}
}

func TestAlertConfigDecryptErrorPropagates(t *testing.T) {
	enc, _ := auth.EncryptString("tgram://tok", cfgMasterKey)
	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{
		"alert_apprise_targets": enc,
	}}, "the-wrong-master-key-also-32-bytes-min!!")
	if _, err := p.AlertConfig(context.Background()); err == nil {
		t.Error("expected decrypt error to propagate")
	}
}
