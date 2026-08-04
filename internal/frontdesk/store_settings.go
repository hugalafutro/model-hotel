package frontdesk

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// GetSettings returns the single settings row. AlertAppriseTargets is the raw
// stored (encrypted) value; the HTTP layer masks it before responding.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	var (
		set          Settings
		alertEnabled int
		oidcEnabled  int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT health_poll_secs, traefik_poll_secs, traefik_stale_secs, event_retention_days, retry_attempts,
		        health_fail_threshold, session_idle_timeout_minutes,
		        alert_enabled, alert_apprise_api_url, alert_apprise_targets, alert_events,
		        oidc_enabled, oidc_issuer_url, oidc_client_id, oidc_client_secret, oidc_public_base_url, oidc_allowed_emails
		 FROM settings WHERE id = 1`,
	).Scan(&set.HealthPollSecs, &set.TraefikPollSecs, &set.TraefikStaleSecs, &set.EventRetentionDays, &set.RetryAttempts,
		&set.HealthFailThreshold, &set.SessionIdleTimeoutMinutes,
		&alertEnabled, &set.AlertAppriseAPIURL, &set.AlertAppriseTargets, &set.AlertEvents,
		&oidcEnabled, &set.OidcIssuerURL, &set.OidcClientID, &set.OidcClientSecret, &set.OidcPublicBaseURL, &set.OidcAllowedEmails)
	if err != nil {
		return Settings{}, fmt.Errorf("frontdesk: get settings: %w", err)
	}
	set.AlertEnabled = alertEnabled != 0
	set.OidcEnabled = oidcEnabled != 0
	return set, nil
}

// UpdateSettings replaces the single settings row after validating bounds.
func (s *Store) UpdateSettings(ctx context.Context, set Settings) error {
	if set.HealthPollSecs < 1 || set.TraefikPollSecs < 1 || set.TraefikStaleSecs < 1 {
		return fmt.Errorf("%w: poll/stale intervals must be at least 1 second", ErrValidation)
	}
	if set.EventRetentionDays < 1 {
		return fmt.Errorf("%w: event retention must be at least 1 day", ErrValidation)
	}
	if set.RetryAttempts < 0 {
		return fmt.Errorf("%w: retry attempts cannot be negative", ErrValidation)
	}
	if set.HealthFailThreshold < 1 {
		return fmt.Errorf("%w: health fail threshold must be at least 1", ErrValidation)
	}
	if set.SessionIdleTimeoutMinutes < 0 || set.SessionIdleTimeoutMinutes > 240 {
		return fmt.Errorf("%w: session idle timeout must be between 0 and 240 minutes", ErrValidation)
	}
	alertEnabled := 0
	if set.AlertEnabled {
		alertEnabled = 1
	}
	oidcEnabled := 0
	if set.OidcEnabled {
		oidcEnabled = 1
	}
	// AlertAppriseTargets and OidcClientSecret are written as-is: the HTTP layer has
	// already encrypted a new value or preserved the existing ciphertext for a
	// masked submission.
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET health_poll_secs = ?, traefik_poll_secs = ?, traefik_stale_secs = ?,
		 event_retention_days = ?, retry_attempts = ?, health_fail_threshold = ?, session_idle_timeout_minutes = ?,
		 alert_enabled = ?, alert_apprise_api_url = ?, alert_apprise_targets = ?, alert_events = ?,
		 oidc_enabled = ?, oidc_issuer_url = ?, oidc_client_id = ?, oidc_client_secret = ?,
		 oidc_public_base_url = ?, oidc_allowed_emails = ? WHERE id = 1`,
		set.HealthPollSecs, set.TraefikPollSecs, set.TraefikStaleSecs,
		set.EventRetentionDays, set.RetryAttempts, set.HealthFailThreshold, set.SessionIdleTimeoutMinutes,
		alertEnabled, set.AlertAppriseAPIURL, set.AlertAppriseTargets, set.AlertEvents,
		oidcEnabled, set.OidcIssuerURL, set.OidcClientID, set.OidcClientSecret,
		set.OidcPublicBaseURL, set.OidcAllowedEmails,
	)
	if err != nil {
		return fmt.Errorf("frontdesk: update settings: %w", err)
	}
	return nil
}

// SetAlertEvents rewrites only the enabled-events CSV, leaving every other
// settings column (including the encrypted Apprise target and the OIDC client
// secret) untouched. The operator alert picker uses this so flipping one event
// never round-trips a stored secret through GET/UpdateSettings. Callers hold
// settingsMu to serialize with putSettings' read-merge-write.
func (s *Store) SetAlertEvents(ctx context.Context, csv string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET alert_events = ? WHERE id = 1`, csv)
	if err != nil {
		return fmt.Errorf("frontdesk: set alert events: %w", err)
	}
	return nil
}

// AutoSyncConfig is the operator's automatic config-propagation setup: a master
// on/off plus the designated source-of-truth member.
type AutoSyncConfig struct {
	Enabled   bool   `json:"enabled"`
	PrimaryID string `json:"primary_id"`
	// Gen is the rearm generation: every change to what the fleet is supposed to
	// hold (a member add or removal, a token update, an enable, a primary repoint)
	// bumps it. A convergence pass captures it before reading the member list and
	// re-checks it before each mutation, so a pass still in flight for the previous
	// primary or member list aborts instead of writing a stale config. Not surfaced.
	Gen int64 `json:"-"`
}

// GetAutoSync reads the automatic config-sync setup from the settings row.
func (s *Store) GetAutoSync(ctx context.Context) (AutoSyncConfig, error) {
	var (
		cfg     AutoSyncConfig
		enabled int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT auto_sync_enabled, auto_sync_primary_id, auto_sync_gen FROM settings WHERE id = 1`,
	).Scan(&enabled, &cfg.PrimaryID, &cfg.Gen)
	if err != nil {
		return AutoSyncConfig{}, fmt.Errorf("frontdesk: get auto-sync: %w", err)
	}
	cfg.Enabled = enabled != 0
	return cfg, nil
}

// SetAutoSync persists the operator's auto-sync choice (enabled + designated
// primary) and bumps the rearm generation in the same write. Enabling auto-sync
// or repointing the primary redefines what the fleet is supposed to hold, so a
// pass still in flight for the old primary must abort rather than finish pushing
// a config the operator has just replaced.
func (s *Store) SetAutoSync(ctx context.Context, enabled bool, primaryID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET auto_sync_enabled = ?, auto_sync_primary_id = ?,
			auto_sync_gen = auto_sync_gen + 1 WHERE id = 1`,
		boolToInt(enabled), primaryID,
	)
	if err != nil {
		return fmt.Errorf("frontdesk: set auto-sync: %w", err)
	}
	return nil
}

// SetAutoSyncGuarded persists the auto-sync choice while enforcing the repoint
// guard in the same statement that writes, so there is no read-modify-write
// window a concurrent repoint could slip through. When the caller is authorized
// (tokenValid, a valid admin token), the choice is written unconditionally.
// Otherwise the write only applies when it does not repoint an already-configured
// primary: either none is set yet, or the request leaves the primary unchanged
// (e.g. just toggling enabled). Reports whether the row was updated; false means
// the change needed admin confirmation (or lost a concurrent repoint) and the
// caller must refuse it. Bumps the rearm generation like SetAutoSync, for the
// same reason.
func (s *Store) SetAutoSyncGuarded(ctx context.Context, enabled bool, primaryID string, tokenValid bool) (bool, error) {
	// auto_sync_enabled rules, evaluated in order against the row's pre-update
	// primary (SQLite reads SET right-hand sides from the original row):
	//   - clearing the primary (new primary empty) forces it off: auto-sync cannot
	//     run without a primary, so this holds the invariant regardless of the
	//     request's flag and independent of any concurrent enable.
	//   - a first set (no primary yet) or an unchanged-primary toggle honors the
	//     requested value: these are the enable/disable control itself.
	//   - a true repoint (new primary differs from the stored one) keeps the stored
	//     value, so a confirmed primary change can never overwrite an enable/disable
	//     another operator made concurrently.
	const set = `UPDATE settings SET
		auto_sync_primary_id = ?,
		auto_sync_enabled = CASE
			WHEN ? = '' THEN 0
			WHEN auto_sync_primary_id = '' OR auto_sync_primary_id = ? THEN ?
			ELSE auto_sync_enabled
		END,
		auto_sync_gen = auto_sync_gen + 1
	WHERE id = 1`
	query := set
	args := []any{primaryID, primaryID, primaryID, boolToInt(enabled)}
	if !tokenValid {
		// Unauthorized writes may not repoint a configured primary: apply only when
		// none is set yet or the primary is left unchanged.
		query += ` AND (auto_sync_primary_id = '' OR auto_sync_primary_id = ?)`
		args = append(args, primaryID)
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("frontdesk: set auto-sync (guarded): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("frontdesk: set auto-sync (guarded) rows: %w", err)
	}
	return n > 0, nil
}

// AutoSyncGen returns the current rearm generation. It is a cheap read an
// in-flight convergence pass uses to notice a rearm (member add, token update,
// enable, or repoint) landed and stop before it pushes a now-stale primary
// export to any further member.
func (s *Store) AutoSyncGen(ctx context.Context) (int64, error) {
	var gen int64
	err := s.db.QueryRowContext(ctx,
		`SELECT auto_sync_gen FROM settings WHERE id = 1`,
	).Scan(&gen)
	if err != nil {
		return 0, fmt.Errorf("frontdesk: read auto-sync gen: %w", err)
	}
	return gen, nil
}

// RearmAutoSync bumps the rearm generation, so a convergence pass already in
// flight aborts rather than pushing a config built for a member list or a
// primary that has since changed. Called when the fleet's membership or the
// designated primary changes.
func (s *Store) RearmAutoSync(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET auto_sync_gen = auto_sync_gen + 1 WHERE id = 1`,
	)
	if err != nil {
		return fmt.Errorf("frontdesk: rearm auto-sync: %w", err)
	}
	return nil
}

// SetMemberLastSync stamps when Front Desk last applied config to a member and
// why, for the Members table "Last Config Sync" column.
func (s *Store) SetMemberLastSync(ctx context.Context, id string, at time.Time, reason string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE members SET last_config_sync_at = ?, last_config_sync_reason = ? WHERE id = ?`,
		at.UTC().UnixNano(), reason, id,
	)
	return affectedOrNotFound(res, err)
}
