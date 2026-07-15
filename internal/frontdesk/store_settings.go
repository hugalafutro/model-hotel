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
// on/off plus the designated source-of-truth member. LastHash is the internal
// drift marker (the primary config hash last applied to the fleet) and is never
// surfaced to the UI.
type AutoSyncConfig struct {
	Enabled   bool   `json:"enabled"`
	PrimaryID string `json:"primary_id"`
	LastHash  string `json:"-"`
	// Gen is the rearm generation: every write that clears LastHash bumps it. A
	// convergence pass captures it at the start and records its hash only if it is
	// unchanged, so a rearm that lands mid-pass cannot be clobbered. Not surfaced.
	Gen int64 `json:"-"`
}

// GetAutoSync reads the automatic config-sync setup from the settings row.
func (s *Store) GetAutoSync(ctx context.Context) (AutoSyncConfig, error) {
	var (
		cfg     AutoSyncConfig
		enabled int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT auto_sync_enabled, auto_sync_primary_id, auto_sync_last_hash, auto_sync_gen FROM settings WHERE id = 1`,
	).Scan(&enabled, &cfg.PrimaryID, &cfg.LastHash, &cfg.Gen)
	if err != nil {
		return AutoSyncConfig{}, fmt.Errorf("frontdesk: get auto-sync: %w", err)
	}
	cfg.Enabled = enabled != 0
	return cfg, nil
}

// SetAutoSync persists the operator's auto-sync choice (enabled + designated
// primary) and clears the last-applied hash in the same write. Resetting the
// marker re-arms the poller: without it, re-enabling auto-sync or switching to a
// primary whose config hash already equals the stored value would return early on
// the next tick and never run a convergence pass, leaving replicas that drifted
// while sync was off (or that should now follow the new primary) stale until the
// primary's config next changed.
func (s *Store) SetAutoSync(ctx context.Context, enabled bool, primaryID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET auto_sync_enabled = ?, auto_sync_primary_id = ?,
			auto_sync_last_hash = '', auto_sync_gen = auto_sync_gen + 1 WHERE id = 1`,
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
// caller must refuse it. Clears the last-applied hash like SetAutoSync, for the
// same re-arm reason.
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
		auto_sync_last_hash = '',
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

// RecordAutoSyncHash records the primary config hash a convergence pass just
// applied to the fleet, so the next tick can detect a change cheaply. The write
// is guarded by gen: it applies only when the rearm generation still matches the
// value the pass captured before it read the member list. If a rearm (member
// add, token update, enable, or repoint) landed mid-pass it bumped the
// generation, the write no-ops (applied=false), the cleared marker stands, and
// the next tick re-converges with the fresh member list or primary. This stops a
// slow older pass from clobbering a deliberate rearm.
func (s *Store) RecordAutoSyncHash(ctx context.Context, hash string, gen int64) (applied bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE settings SET auto_sync_last_hash = ? WHERE id = 1 AND auto_sync_gen = ?`,
		hash, gen,
	)
	if err != nil {
		return false, fmt.Errorf("frontdesk: record auto-sync hash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("frontdesk: record auto-sync hash rows: %w", err)
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

// RearmAutoSync clears the last-applied config hash and bumps the rearm
// generation in one statement, so the auto-sync loop runs a fresh pass and any
// convergence pass already in flight cannot record its (now stale) hash over the
// clear. Called when the fleet's membership or the designated primary changes.
func (s *Store) RearmAutoSync(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE settings SET auto_sync_last_hash = '', auto_sync_gen = auto_sync_gen + 1 WHERE id = 1`,
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
