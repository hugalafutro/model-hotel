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
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"

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
	// ErrLastActiveMember is returned when draining a member would leave the fleet
	// with no active (routable) members: the Traefik backend pool would be empty
	// and all proxy traffic would fail. A distinct sentinel so the server maps it
	// to 409 with a stable machine code rather than a generic validation 400.
	ErrLastActiveMember = errors.New("frontdesk: cannot drain the last active member")
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
