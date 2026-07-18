// Package user implements dashboard user accounts for multi-user support:
// the users table, argon2id password hashing, and the feature-grant catalog.
// The env admin token stays outside this package as a break-glass superadmin.
package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role separates full operators from grant-limited users.
type Role string

// Roles: admins get everything (grant checks bypassed), users only what their
// grant list allows.
const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// ErrNotFound is returned when no user matches the lookup.
var ErrNotFound = errors.New("user not found")

// ErrSSOMismatch is returned when a verified SSO email matches an account that
// is already bound to a different provider identity. It denies cross-provider
// takeover: an account first entered via one provider can never be assumed by
// another provider that merely asserts the same verified email.
var ErrSSOMismatch = errors.New("sso identity mismatch")

// User is a dashboard account. PasswordHash never leaves the backend.
type User struct {
	ID           uuid.UUID  `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"display_name"`
	Email        *string    `json:"email"`
	PasswordHash string     `json:"-"`
	Role         Role       `json:"role"`
	Grants       []string   `json:"grants"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	// Aggregate proxy limits across every virtual key this user owns.
	// NULL = no cap; there is no global-settings fallback for these.
	RateLimitRPS   *float64 `json:"rate_limit_rps"`
	RateLimitBurst *int     `json:"rate_limit_burst"`
	RateLimitTPM   *int     `json:"rate_limit_tpm"`
	// TotpEnabled reports whether the user has a confirmed second factor.
	// Derived from user_totp by the API layer (ListUsers), never scanned from
	// the users table; false in Create/Update responses (the UI refetches).
	TotpEnabled bool `json:"totp_enabled"`
	// SSOProvider/SSOSubject lock the account to one external identity. NULL
	// until the account's first OIDC/GitHub login, then the account can only be
	// re-entered by that same (provider, subject) -- a second provider asserting
	// the same verified email is rejected (see ResolveSSOIdentity). Never
	// exposed over the API.
	SSOProvider *string `json:"-"`
	SSOSubject  *string `json:"-"`
}

// Repository provides CRUD over the users table.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a users repository on the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const userColumns = `id, username, display_name, email, password_hash, role, grants, enabled, created_at, updated_at, last_login_at, rate_limit_rps, rate_limit_burst, rate_limit_tpm, sso_provider, sso_subject`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash,
		&u.Role, &u.Grants, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
		&u.RateLimitRPS, &u.RateLimitBurst, &u.RateLimitTPM, &u.SSOProvider, &u.SSOSubject)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// NormalizeEmail lowercases and trims an email for storage and lookup, mapping
// empty input to nil so the partial unique index ignores account rows without
// an SSO binding.
func NormalizeEmail(email *string) *string {
	if email == nil {
		return nil
	}
	e := strings.ToLower(strings.TrimSpace(*email))
	if e == "" {
		return nil
	}
	return &e
}

// Create inserts a new user. passwordHash must already be argon2id-encoded.
func (r *Repository) Create(ctx context.Context, username, displayName string, email *string, passwordHash string, role Role, grants []string, limits Limits) (*User, error) {
	if grants == nil {
		grants = []string{}
	}
	return scanUser(r.pool.QueryRow(ctx,
		`INSERT INTO users (username, display_name, email, password_hash, role, grants, rate_limit_rps, rate_limit_burst, rate_limit_tpm)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+userColumns,
		username, displayName, NormalizeEmail(email), passwordHash, role, grants,
		limits.RPS, limits.Burst, limits.TPM))
}

// List returns all users, newest first.
func (r *Repository) List(ctx context.Context) ([]*User, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Get retrieves a user by ID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

// GetByUsername retrieves a user by exact username (login path).
func (r *Repository) GetByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

// GetByEmail retrieves a user by normalized email (SSO mapping path).
func (r *Repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	e := NormalizeEmail(&email)
	if e == nil {
		return nil, ErrNotFound
	}
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, *e))
}

// ResolveSSOIdentity binds an OIDC/GitHub login to a user account by verified
// email while enforcing exactly one external identity per account. On an
// account's first SSO login the (provider, subject) is recorded
// (trust-on-first-use); afterwards a login whose (provider, subject) differs is
// rejected with ErrSSOMismatch even when the verified email matches, which
// stops a second, lower-trust provider from impersonating the account. Unknown
// emails and disabled accounts yield ErrNotFound. provider is a short constant
// ("oidc", "github"); subject is the provider's stable, opaque per-user id. The
// returned bool is true only when this call recorded a first-ever binding, so
// callers can surface that (audit/alert) distinctly from a routine re-login.
func (r *Repository) ResolveSSOIdentity(ctx context.Context, provider, subject, email string) (*User, bool, error) {
	e := NormalizeEmail(&email)
	if e == nil {
		return nil, false, ErrNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once the tx commits

	// FOR UPDATE serializes concurrent first-logins for the same email so the
	// "is it bound yet?" check and the bind are atomic; without the row lock two
	// different identities could both observe NULL and each overwrite the other.
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1 FOR UPDATE`, *e))
	if err != nil {
		return nil, false, err // ErrNotFound when no row matches
	}
	if !u.Enabled {
		return nil, false, ErrNotFound
	}

	bound := false
	switch {
	case u.SSOProvider == nil:
		// Trust-on-first-use: bind this identity to the account. A unique-index
		// conflict means the identity is already bound to a different account,
		// which is itself a cross-account mismatch, not a fresh binding.
		if _, err := tx.Exec(ctx,
			`UPDATE users SET sso_provider = $2, sso_subject = $3 WHERE id = $1`,
			u.ID, provider, subject); err != nil {
			if isUniqueViolation(err) {
				return nil, false, ErrSSOMismatch
			}
			return nil, false, err
		}
		u.SSOProvider = &provider
		u.SSOSubject = &subject
		bound = true
	case *u.SSOProvider != provider || u.SSOSubject == nil || *u.SSOSubject != subject:
		return nil, false, ErrSSOMismatch
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return u, bound, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505"
	}
	return false
}

// HasEnabled reports whether at least one enabled user exists, so the login
// UI knows whether to offer the username/password form.
func (r *Repository) HasEnabled(ctx context.Context) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE enabled)`).Scan(&exists)
	return exists, err
}

// Update rewrites the mutable profile fields (not the password). Limits are
// always written: a nil pointer clears the cap, matching the virtual-key
// update semantics (the UI sends null when the field is emptied).
func (r *Repository) Update(ctx context.Context, id uuid.UUID, username, displayName string, email *string, role Role, grants []string, enabled bool, limits Limits) (*User, error) {
	if grants == nil {
		grants = []string{}
	}
	return scanUser(r.pool.QueryRow(ctx,
		`UPDATE users
		 SET username = $2, display_name = $3, email = $4, role = $5, grants = $6, enabled = $7,
		     rate_limit_rps = $8, rate_limit_burst = $9, rate_limit_tpm = $10, updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+userColumns,
		id, username, displayName, NormalizeEmail(email), role, grants, enabled,
		limits.RPS, limits.Burst, limits.TPM))
}

// Limits bundles the per-user aggregate limit fields for Create/Update.
type Limits struct {
	RPS   *float64
	Burst *int
	TPM   *int
}

// SetPassword replaces the stored hash (admin reset or user change).
func (r *Repository) SetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`, id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchLastLogin records a successful login without bumping updated_at.
func (r *Repository) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

// Delete removes a user. Callers must also revoke the user's sessions.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
