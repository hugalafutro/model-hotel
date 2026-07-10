// Package frontdesk implements the HA "Front Desk" control plane: the member
// list, control-plane event log, settings, Traefik dynamic-config generation,
// health/version polling, and the admin UI server. It is never in the request
// path; its failure mode is "membership changes temporarily impossible," not
// "gateway down."
//
// Storage is a single embedded SQLite file (modernc.org/sqlite, pure Go, no
// CGO). The same file also backs the reused webauthn and totp logic via the
// SQLite Store implementations in authstore.go, so Front Desk needs no Postgres.
package frontdesk

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Sentinel errors. The server layer maps ErrValidation/ErrDuplicateURL to 400
// and ErrNotFound to 404.
var (
	// ErrNotFound is returned when a member (or other row) does not exist.
	ErrNotFound = errors.New("frontdesk: not found")
	// ErrDuplicateURL is returned when a member URL collides with an existing one.
	ErrDuplicateURL = errors.New("frontdesk: a member with this URL already exists")
	// ErrValidation wraps input validation failures.
	ErrValidation = errors.New("frontdesk: validation failed")
	// ErrInsecureURL is returned when a member URL uses plain http and plain http
	// is not allowed. It is a distinct sentinel (not plain ErrValidation) so the
	// server can hand the frontend a stable machine code instead of English text.
	ErrInsecureURL = errors.New("frontdesk: member url must use https")
)

// MemberState is the operational state of a member as the control plane sees it.
type MemberState string

const (
	// StateActive members are included in the generated Traefik backend pool.
	StateActive MemberState = "active"
	// StateDrained members are excluded from new traffic; in-flight streams finish.
	StateDrained MemberState = "drained"
)

// Member is a backend Model Hotel instance registered with Front Desk. The
// stored admin token is never exposed; HasToken reports only its presence.
type Member struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	URL       string      `json:"url"`
	State     MemberState `json:"state"`
	HasToken  bool        `json:"has_token"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	// LastConfigSyncAt is when Front Desk last applied config to this member
	// (wizard or automatic); nil until the first sync. LastConfigSyncReason
	// explains why (e.g. the primary's config changed). Both power the Members
	// table "Last Config Sync" column.
	LastConfigSyncAt     *time.Time `json:"last_config_sync_at,omitempty"`
	LastConfigSyncReason string     `json:"last_config_sync_reason,omitempty"`
	// InstanceID is the stable identity of the model-hotel instance behind this
	// member (from its /api/system). Empty until Front Desk has verified the
	// member at least once. Used to detect the same instance under another URL.
	InstanceID string `json:"instance_id,omitempty"`
}

// Settings shape the generated Traefik config and the pollers. The single row
// (id = 1) is seeded with defaults by the first migration.
type Settings struct {
	HealthPollSecs     int `json:"health_poll_secs"`
	TraefikPollSecs    int `json:"traefik_poll_secs"`
	TraefikStaleSecs   int `json:"traefik_stale_secs"`
	EventRetentionDays int `json:"event_retention_days"`
	RetryAttempts      int `json:"retry_attempts"`

	// HealthFailThreshold is the number of consecutive failed health polls a
	// member must accrue before Front Desk reports it down (an error event plus,
	// by default, an Apprise alert). It damps the reachability flap of a routine
	// container rebuild; recovery is immediate. The same threshold governs the
	// Traefik UP->DOWN badge flip. Bounded to at least 1.
	HealthFailThreshold int `json:"health_fail_threshold"`

	// Admin-UI inactivity auto-logout window in minutes; 0 disables it. Consumed
	// by the frontend (useIdleLogout); the server only stores and bounds it.
	SessionIdleTimeoutMinutes int `json:"session_idle_timeout_minutes"`

	// Outbound Apprise alerting (HA operator notifications). AlertAppriseTargets
	// is stored encrypted at rest (auth.EncryptString) and masked at the API
	// boundary; the store layer reads/writes the raw column value. AlertEvents is
	// the CSV of enabled event Types (the per-event picker).
	AlertEnabled        bool   `json:"alert_enabled"`
	AlertAppriseAPIURL  string `json:"alert_apprise_api_url"`
	AlertAppriseTargets string `json:"alert_apprise_targets"`
	AlertEvents         string `json:"alert_events"`

	// OIDC SSO admin login (a fourth login path, reusing the shared adminauth
	// handler). OidcClientSecret is stored encrypted at rest (auth.EncryptString)
	// and masked at the API boundary, like AlertAppriseTargets; the store layer
	// reads/writes the raw column value. The frontdeskOIDCSettings adapter exposes
	// these to adminauth.OIDCHandler via the GetBool/GetWithDefault contract.
	OidcEnabled       bool   `json:"oidc_enabled"`
	OidcIssuerURL     string `json:"oidc_issuer_url"`
	OidcClientID      string `json:"oidc_client_id"`
	OidcClientSecret  string `json:"oidc_client_secret"`
	OidcPublicBaseURL string `json:"oidc_public_base_url"`
	OidcAllowedEmails string `json:"oidc_allowed_emails"`
}

// Event is a control-plane fact (membership change, health transition, config
// lifecycle, auth event). It never carries request or prompt content.
type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Severity  string         `json:"severity"`
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	MemberID  string         `json:"member_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// EventFilter narrows ListEvents. Zero-value fields are ignored.
type EventFilter struct {
	MemberID string
	Type     string
	Severity string
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

// Store is the SQLite-backed persistence for the control plane. The underlying
// *sql.DB is shared with the webauthn and totp SQLite stores (authstore.go).
type Store struct {
	db               *sql.DB
	masterKey        string
	allowHTTPMembers bool
}

// Open opens (creating if absent) the SQLite database at path and runs the
// embedded migrations. masterKey encrypts stored member admin tokens at rest;
// it may be empty, in which case CreateMember/SetMemberToken reject a non-empty
// token (so a token is never written in the clear). allowHTTPMembers permits
// plain-http member URLs; when false (the default), member URLs must be https so
// the admin token is never sent in the clear across the network.
func Open(path, masterKey string, allowHTTPMembers bool) (*Store, error) {
	// WAL + a generous busy timeout keep the low-traffic control plane free of
	// "database is locked" under concurrent pollers and request handlers.
	// foreign_keys(on) makes any future ON DELETE CASCADE actually enforced on
	// SQLite (it is off by default), so a dev relying on Postgres parity isn't
	// silently surprised; today cascades are app-level in both backends.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: open sqlite: %w", err)
	}

	s := &Store{db: db, masterKey: masterKey, allowHTTPMembers: allowHTTPMembers}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// DB returns the shared connection handle for the auth stores in this package.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`,
	); err != nil {
		return fmt.Errorf("frontdesk: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("frontdesk: read migrations: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var exists bool
		if err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = ?)`, e.Name(),
		).Scan(&exists); err != nil {
			return fmt.Errorf("frontdesk: check migration %s: %w", e.Name(), err)
		}
		if exists {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return fmt.Errorf("frontdesk: read migration %s: %w", e.Name(), err)
		}
		if err := s.applyMigration(ctx, e.Name(), string(content)); err != nil {
			return err
		}
		debuglog.Info("frontdesk: applied migration", "name", e.Name())
	}
	return nil
}

// applyMigration runs one migration's statements and records it in
// schema_migrations within a single transaction. Bundling the two means a crash
// between them can never leave a migration applied-but-unrecorded, which on the
// next start would re-run it: harmless for an idempotent CREATE ... IF NOT
// EXISTS, but fatal for an ALTER TABLE ADD COLUMN (duplicate column, bricked
// binary). SQLite executes DDL transactionally, so a failure rolls the whole
// migration back.
func (s *Store) applyMigration(ctx context.Context, name, content string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("frontdesk: begin migration %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op
	if _, err := tx.ExecContext(ctx, content); err != nil {
		return fmt.Errorf("frontdesk: apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().UTC().UnixNano(),
	); err != nil {
		return fmt.Errorf("frontdesk: record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("frontdesk: commit migration %s: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// CreateMember validates and inserts a new member. name must be non-empty and
// rawURL must be a valid http(s) URL with a host; the URL is normalized (scheme
// lowercased, trailing slash trimmed) and deduped. token is optional; when set
// it is encrypted at rest with the store master key.
func (s *Store) CreateMember(ctx context.Context, name, rawURL, token string) (*Member, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	normURL, err := normalizeMemberURL(rawURL, s.allowHTTPMembers)
	if err != nil {
		return nil, err
	}

	cipher, nonce, salt, err := s.encryptToken(token)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	now := time.Now().UTC().UnixNano()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO members (id, name, url, state, token_cipher, token_nonce, token_salt, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, normURL, string(StateActive), cipher, nonce, salt, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateURL
		}
		return nil, fmt.Errorf("frontdesk: insert member: %w", err)
	}
	return s.GetMember(ctx, id)
}

// ListMembers returns all members ordered by creation time.
func (s *Store) ListMembers(ctx context.Context) ([]*Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, state, token_cipher, created_at, updated_at, last_config_sync_at, last_config_sync_reason, instance_id FROM members ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: list members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []*Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetMember returns one member by id, or ErrNotFound.
func (s *Store) GetMember(ctx context.Context, id string) (*Member, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, state, token_cipher, created_at, updated_at, last_config_sync_at, last_config_sync_reason, instance_id FROM members WHERE id = ?`, id,
	)
	m, err := scanMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// RenameMember updates a member's display name.
func (s *Store) RenameMember(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	return s.touchMember(ctx, `UPDATE members SET name = ?, updated_at = ? WHERE id = ?`, id, name)
}

// SetMemberToken encrypts and stores a member's admin token. An empty token
// clears it (no token stored).
func (s *Store) SetMemberToken(ctx context.Context, id, token string) error {
	cipher, nonce, salt, err := s.encryptToken(token)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE members SET token_cipher = ?, token_nonce = ?, token_salt = ?, updated_at = ? WHERE id = ?`,
		cipher, nonce, salt, time.Now().UTC().UnixNano(), id,
	)
	return affectedOrNotFound(res, err)
}

// SetMemberState sets a member's state (active or drained).
func (s *Store) SetMemberState(ctx context.Context, id string, state MemberState) error {
	if state != StateActive && state != StateDrained {
		return fmt.Errorf("%w: invalid state %q", ErrValidation, state)
	}
	return s.touchMember(ctx, `UPDATE members SET state = ?, updated_at = ? WHERE id = ?`, id, string(state))
}

// DeleteMember removes a member by id.
func (s *Store) DeleteMember(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM members WHERE id = ?`, id)
	if err := affectedOrNotFound(res, err); err != nil {
		return err
	}
	// If the removed member was the designated auto-sync primary, clear the
	// pointer so the auto-sync loop stops treating a now-gone member as the
	// source of truth. Best-effort: a failure here only leaves a dangling id the
	// loop already guards against.
	_, _ = s.db.ExecContext(ctx, `UPDATE settings SET auto_sync_primary_id = '' WHERE auto_sync_primary_id = ?`, id)
	return nil
}

// SetMemberInstanceID records the stable identity Front Desk learned for a
// member from its /api/system. Idempotent; a no-op if the row is gone.
func (s *Store) SetMemberInstanceID(ctx context.Context, id, instanceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE members SET instance_id = ? WHERE id = ?`, instanceID, id)
	return err
}

// DeleteMemberIfNotPrimary removes a member by id, but never the configured
// fleet primary. The primary is the config source of truth and can only be
// changed by re-running the Fleet Sync wizard (a token-gated repoint), so there
// is deliberately no way to delete it directly, with or without a token. The
// primary-status check and the delete are a single atomic SQL statement, so a
// concurrent primary reassignment cannot slip between the check and the delete
// (the TOCTOU window a two-step GetAutoSync + DeleteMember would have). Returns
// applied=false when the member is the current primary; the caller should
// respond 409 and point the operator at the wizard.
func (s *Store) DeleteMemberIfNotPrimary(ctx context.Context, id string) (applied bool, err error) {
	// The delete and its ghost-state cleanup run in one transaction, so a crash
	// mid-way can never leave a fleet_sync_state row naming a member that was
	// already removed (exactly the ghost that made the old badge misreport who the
	// primary was).
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("frontdesk: begin delete member: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op

	// Delete only if the member is NOT the fleet primary. The sub-query makes the
	// check and the delete a single atomic statement, so a concurrent primary
	// reassignment cannot slip between the check and the delete.
	res, err := tx.ExecContext(ctx, `
		DELETE FROM members
		WHERE id = ?
		  AND id NOT IN (SELECT auto_sync_primary_id FROM settings WHERE id = 1)`,
		id)
	if err != nil {
		return false, fmt.Errorf("frontdesk: delete member: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	// A removed non-primary member must not linger as the auto-sync primary (it
	// never should, but stay defensive) nor as the stale "last run" marker.
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET auto_sync_primary_id = '' WHERE auto_sync_primary_id = ?`, id); err != nil {
		return false, fmt.Errorf("frontdesk: clear auto-sync primary: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fleet_sync_state SET primary_id = '', primary_name = '' WHERE id = 1 AND primary_id = ?`, id); err != nil {
		return false, fmt.Errorf("frontdesk: clear ghost fleet state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("frontdesk: commit delete member: %w", err)
	}
	return true, nil
}

// MemberToken decrypts and returns a member's stored admin token. ok is false
// when no token is stored for the member.
func (s *Store) MemberToken(ctx context.Context, id string) (token string, ok bool, err error) {
	var cipher, nonce, salt []byte
	row := s.db.QueryRowContext(ctx, `SELECT token_cipher, token_nonce, token_salt FROM members WHERE id = ?`, id)
	if err := row.Scan(&cipher, &nonce, &salt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, fmt.Errorf("frontdesk: load member token: %w", err)
	}
	if len(cipher) == 0 {
		return "", false, nil
	}
	plain, err := auth.Decrypt(cipher, nonce, salt, s.masterKey)
	if err != nil {
		return "", false, fmt.Errorf("frontdesk: decrypt member token: %w", err)
	}
	return plain, true, nil
}

// touchMember runs an UPDATE that sets one column plus updated_at and maps a
// zero-row result to ErrNotFound. The query must take (value, updated_at, id).
func (s *Store) touchMember(ctx context.Context, query, id, value string) error {
	res, err := s.db.ExecContext(ctx, query, value, time.Now().UTC().UnixNano(), id)
	return affectedOrNotFound(res, err)
}

// encryptToken encrypts a non-empty token with the store master key. An empty
// token yields three nil slices (cleared). A non-empty token with no master key
// is a validation error so plaintext is never written.
func (s *Store) encryptToken(token string) (cipher, nonce, salt []byte, err error) {
	if token == "" {
		return nil, nil, nil, nil
	}
	if s.masterKey == "" {
		return nil, nil, nil, fmt.Errorf("%w: a master key is required to store a member admin token", ErrValidation)
	}
	kp, err := auth.Encrypt(token, s.masterKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("frontdesk: encrypt member token: %w", err)
	}
	return kp.Ciphertext, kp.Nonce, kp.Salt, nil
}

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

// ---------------------------------------------------------------------------
// Fleet sync state
// ---------------------------------------------------------------------------

// FleetSyncState records the last successful fleet-sync wizard run, so the
// wizard can show that it has run before (and against which primary) instead of
// looking untouched after a container rebuild.
type FleetSyncState struct {
	LastRunAt   time.Time `json:"last_run_at"`
	PrimaryID   string    `json:"primary_id"`
	PrimaryName string    `json:"primary_name"`
}

// GetFleetSyncState returns the recorded last-run marker. found is false (with a
// nil error) when the wizard has never recorded a successful run.
func (s *Store) GetFleetSyncState(ctx context.Context) (state FleetSyncState, found bool, err error) {
	var at int64
	err = s.db.QueryRowContext(ctx,
		`SELECT last_run_at, primary_id, primary_name FROM fleet_sync_state WHERE id = 1`,
	).Scan(&at, &state.PrimaryID, &state.PrimaryName)
	if errors.Is(err, sql.ErrNoRows) {
		return FleetSyncState{}, false, nil
	}
	if err != nil {
		return FleetSyncState{}, false, fmt.Errorf("frontdesk: get fleet sync state: %w", err)
	}
	state.LastRunAt = time.Unix(0, at).UTC()
	return state, true, nil
}

// SetFleetSyncState upserts the single-row last-run marker.
func (s *Store) SetFleetSyncState(ctx context.Context, primaryID, primaryName string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fleet_sync_state (id, last_run_at, primary_id, primary_name) VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_run_at = excluded.last_run_at,
		   primary_id = excluded.primary_id, primary_name = excluded.primary_name`,
		at.UTC().UnixNano(), primaryID, primaryName,
	)
	if err != nil {
		return fmt.Errorf("frontdesk: set fleet sync state: %w", err)
	}
	return nil
}

// EnsureFrontdeskID returns this Front Desk's persistent identity, generating
// and storing a UUID on first use. It reads the frontdesk_id column from the
// singleton settings row (id = 1); if empty, it generates a UUID, persists it,
// and returns it. Idempotent: a second call returns the same value. This ID is
// stamped onto every announce so a member can tell which Front Desk owns its
// fleet role (see internal/api/fleet.go Announce).
func (s *Store) EnsureFrontdeskID(ctx context.Context) (string, error) {
	var id string
	if err := s.db.QueryRowContext(ctx,
		`SELECT frontdesk_id FROM settings WHERE id = 1`,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("frontdesk: read frontdesk_id: %w", err)
	}
	if id != "" {
		return id, nil
	}
	id = uuid.NewString()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE settings SET frontdesk_id = ? WHERE id = 1 AND frontdesk_id = ''`,
		id,
	); err != nil {
		return "", fmt.Errorf("frontdesk: persist frontdesk_id: %w", err)
	}
	// Re-read: a concurrent first-caller may have won the guarded UPDATE, in
	// which case our write was a no-op and the stored value is theirs. Either
	// way the row now holds the single agreed-upon ID.
	if err := s.db.QueryRowContext(ctx,
		`SELECT frontdesk_id FROM settings WHERE id = 1`,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("frontdesk: reread frontdesk_id: %w", err)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// InsertEvent persists a control-plane event. ID and CreatedAt are assigned
// when empty. The returned Event carries the persisted ID/timestamp.
func (s *Store) InsertEvent(ctx context.Context, e Event) (Event, error) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	var metaJSON *string
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return Event{}, fmt.Errorf("frontdesk: marshal event metadata: %w", err)
		}
		str := string(b)
		metaJSON = &str
	}
	var memberID *string
	if e.MemberID != "" {
		memberID = &e.MemberID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, type, severity, source, message, metadata, member_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Type, e.Severity, e.Source, e.Message, metaJSON, memberID, e.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return Event{}, fmt.Errorf("frontdesk: insert event: %w", err)
	}
	return e, nil
}

// ListEvents returns events matching the filter (newest first) plus the total
// count of matching rows (ignoring limit/offset) for pagination.
func (s *Store) ListEvents(ctx context.Context, f EventFilter) ([]Event, int, error) {
	where, args := eventWhere(f)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("frontdesk: count events: %w", err)
	}

	//nolint:gosec // `where` is built only from fixed clause strings; all values are bound parameters.
	query := `SELECT id, type, severity, source, message, metadata, member_id, created_at FROM events` + where + ` ORDER BY created_at DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("frontdesk: list events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

// PruneEvents deletes events older than retentionDays and returns the count
// removed.
func (s *Store) PruneEvents(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays < 1 {
		return 0, fmt.Errorf("%w: retention must be at least 1 day", ErrValidation)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixNano()
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("frontdesk: prune events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func eventWhere(f EventFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.MemberID != "" {
		clauses = append(clauses, "member_id = ?")
		args = append(args, f.MemberID)
	}
	if f.Type != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, f.Type)
	}
	if f.Severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, f.Severity)
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.Since.UTC().UnixNano())
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, f.Until.UTC().UnixNano())
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func scanMember(sc scanner) (*Member, error) {
	var (
		m          Member
		state      string
		cipher     []byte
		createdAt  int64
		updatedAt  int64
		lastSyncAt sql.NullInt64
		syncReason string
	)
	if err := sc.Scan(&m.ID, &m.Name, &m.URL, &state, &cipher, &createdAt, &updatedAt, &lastSyncAt, &syncReason, &m.InstanceID); err != nil {
		return nil, err
	}
	m.State = MemberState(state)
	m.HasToken = len(cipher) > 0
	m.CreatedAt = time.Unix(0, createdAt).UTC()
	m.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if lastSyncAt.Valid {
		t := time.Unix(0, lastSyncAt.Int64).UTC()
		m.LastConfigSyncAt = &t
	}
	m.LastConfigSyncReason = syncReason
	return &m, nil
}

func scanEvent(sc scanner) (Event, error) {
	var (
		e         Event
		metaJSON  *string
		memberID  *string
		createdAt int64
	)
	if err := sc.Scan(&e.ID, &e.Type, &e.Severity, &e.Source, &e.Message, &metaJSON, &memberID, &createdAt); err != nil {
		return Event{}, err
	}
	if metaJSON != nil && *metaJSON != "" {
		if err := json.Unmarshal([]byte(*metaJSON), &e.Metadata); err != nil {
			return Event{}, fmt.Errorf("frontdesk: unmarshal event metadata: %w", err)
		}
	}
	if memberID != nil {
		e.MemberID = *memberID
	}
	e.CreatedAt = time.Unix(0, createdAt).UTC()
	return e, nil
}

// normalizeMemberURL validates and canonicalizes a member base URL. When
// allowHTTP is false, plain-http URLs are rejected so the member admin token is
// never transmitted in the clear.
func normalizeMemberURL(raw string, allowHTTP bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: url is required", ErrValidation)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: url is not valid: %w", ErrValidation, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: url must use http or https", ErrValidation)
	}
	if u.Scheme == "http" && !allowHTTP {
		return "", fmt.Errorf("%w; set FRONTDESK_ALLOW_HTTP_MEMBERS=true to allow plain http on a trusted internal network", ErrInsecureURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: url must include a host", ErrValidation)
	}
	// Reject a literal IP that is a known SSRF target (link-local, including the
	// cloud-metadata endpoint, or the unspecified address) at add time for a
	// clear error. Hostnames that resolve to such an address are caught later at
	// dial time by the poller's guarded client (see netguard.go).
	if ip := net.ParseIP(u.Hostname()); ip != nil && isProbeBlockedIP(ip) {
		return "", fmt.Errorf("%w: url host %s is not an allowed address", ErrValidation, u.Hostname())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func affectedOrNotFound(res sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("frontdesk: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite reports constraint failures in the error text.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
