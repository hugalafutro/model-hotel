package frontdesk

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
)

// alertMaskValue is returned to the UI in place of a stored Apprise target so the
// encrypted secret never leaves the server. A PUT echoing this value preserves
// the stored ciphertext; any other value is a new secret to encrypt. Matches the
// main app's secretMaskValue.
const alertMaskValue = "********"

// fdCatalog is Front Desk's alertable-event registry: the per-event picker and
// the dispatcher's gate are both built from it. Every Type is grounded in an
// event Front Desk actually publishes (recordEvent/emit), so the operator never
// sees a checkbox nothing emits. Keep the DefaultOn set in step with the
// alert_events seed in migrations/007_alerts.sql (a test guards the pairing).
var fdCatalog = []alert.EventDef{
	// Member health: the core "is my fleet alive" signal.
	{Type: "health.down", Category: "Health", Severity: "error", DefaultOn: true},
	{Type: "health.up", Category: "Health", Severity: "success", DefaultOn: true},
	// Config sync (manual wizard + auto-sync). A failed push is the headline alert.
	{Type: "config.sync_failed", Category: "Config Sync", Severity: "error", DefaultOn: true},
	{Type: "config.synced", Category: "Config Sync", Severity: "success", DefaultOn: false},
	{Type: "config.auto_synced", Category: "Config Sync", Severity: "success", DefaultOn: false},
	{Type: "config.regenerated", Category: "Config Sync", Severity: "info", DefaultOn: false},
	// Version reads: a persistently failing member URL is surfaced here.
	{Type: "version.fetch_failed", Category: "Member Reads", Severity: "warning", DefaultOn: true},
	{Type: "version.fetch_recovered", Category: "Member Reads", Severity: "success", DefaultOn: false},
	// Traefik dynamic-config staleness.
	{Type: "traefik.stale", Category: "Routing", Severity: "warning", DefaultOn: false},
	// Fleet roster + drain/active changes.
	{Type: "member.added", Category: "Membership", Severity: "info", DefaultOn: false},
	{Type: "member.removed", Category: "Membership", Severity: "warning", DefaultOn: false},
	{Type: "member.state_changed", Category: "Membership", Severity: "info", DefaultOn: false},
}

// alertConfigProvider resolves Front Desk's alert.Config from the settings row,
// decrypting the Apprise target with the Front Desk master key. It implements
// alert.ConfigProvider and is read live on every dispatch so picker/toggle edits
// take effect without a restart.
type alertConfigProvider struct {
	store     *Store
	masterKey string
}

// AlertConfig implements alert.ConfigProvider.
func (p alertConfigProvider) AlertConfig(ctx context.Context) (alert.Config, error) {
	set, err := p.store.GetSettings(ctx)
	if err != nil {
		return alert.Config{}, err
	}
	targets, err := auth.DecryptString(set.AlertAppriseTargets, p.masterKey)
	if err != nil {
		return alert.Config{}, fmt.Errorf("frontdesk: decrypt alert target: %w", err)
	}
	return alert.Config{
		Enabled:    set.AlertEnabled,
		APIBaseURL: set.AlertAppriseAPIURL,
		Targets:    targets,
		// A blank alert_events means the operator deselected everything (the
		// migration seeds the defaults), so nothing fires -- no Go-side fallback.
		Events: alert.ParseEnabled(set.AlertEvents),
	}, nil
}

// APIBaseURL implements alert.ConfigProvider: the apprise-api URL without
// touching the encrypted target, so a reachability probe cannot fail on a
// corrupt secret or rotated master key.
func (p alertConfigProvider) APIBaseURL(ctx context.Context) (string, error) {
	set, err := p.store.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	return set.AlertAppriseAPIURL, nil
}
