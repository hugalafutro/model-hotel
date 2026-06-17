package alert

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

// Setting keys backing the alerting feature.
const (
	keyEnabled    = "alert_enabled"
	keyAPIBaseURL = "alert_apprise_api_url"
	keyTargets    = "alert_apprise_targets" // encrypted at rest
	keyEvents     = "alert_events"          // CSV of enabled event Types
)

// settingsReader is the slice of the settings repository the provider needs.
// *settings.Repository satisfies it.
type settingsReader interface {
	GetBool(ctx context.Context, key string, defaultValue bool) bool
	GetWithDefault(ctx context.Context, key, defaultValue string) string
}

// SettingsConfigProvider resolves alert Config from the settings store,
// decrypting the Apprise target with the master key.
type SettingsConfigProvider struct {
	settings  settingsReader
	masterKey string
}

// NewSettingsConfigProvider builds a ConfigProvider backed by the settings repo.
func NewSettingsConfigProvider(settings settingsReader, masterKey string) *SettingsConfigProvider {
	return &SettingsConfigProvider{settings: settings, masterKey: masterKey}
}

// AlertConfig implements ConfigProvider, reading live settings on each call so
// operator changes (toggles, picker edits) take effect without a restart.
func (p *SettingsConfigProvider) AlertConfig(ctx context.Context) (Config, error) {
	stored := p.settings.GetWithDefault(ctx, keyTargets, "")
	targets, err := auth.DecryptString(stored, p.masterKey)
	if err != nil {
		return Config{}, fmt.Errorf("decrypt alert target: %w", err)
	}

	// An unset alert_events key (first run) seeds the picker from the catalog
	// defaults; an explicitly-empty value means the operator deselected
	// everything and nothing fires. GetWithDefault distinguishes the two.
	enabledCSV := p.settings.GetWithDefault(ctx, keyEvents, DefaultEnabledCSV())

	return Config{
		Enabled:    p.settings.GetBool(ctx, keyEnabled, false),
		APIBaseURL: p.settings.GetWithDefault(ctx, keyAPIBaseURL, ""),
		Targets:    targets,
		Events:     ParseEnabled(enabledCSV),
	}, nil
}
