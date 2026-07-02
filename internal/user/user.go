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
}

// Repository provides CRUD over the users table.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a users repository on the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const userColumns = `id, username, display_name, email, password_hash, role, grants, enabled, created_at, updated_at, last_login_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash,
		&u.Role, &u.Grants, &u.Enabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
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
func (r *Repository) Create(ctx context.Context, username, displayName string, email *string, passwordHash string, role Role, grants []string) (*User, error) {
	if grants == nil {
		grants = []string{}
	}
	return scanUser(r.pool.QueryRow(ctx,
		`INSERT INTO users (username, display_name, email, password_hash, role, grants)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+userColumns,
		username, displayName, NormalizeEmail(email), passwordHash, role, grants))
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

// Update rewrites the mutable profile fields (not the password).
func (r *Repository) Update(ctx context.Context, id uuid.UUID, username, displayName string, email *string, role Role, grants []string, enabled bool) (*User, error) {
	if grants == nil {
		grants = []string{}
	}
	return scanUser(r.pool.QueryRow(ctx,
		`UPDATE users
		 SET username = $2, display_name = $3, email = $4, role = $5, grants = $6, enabled = $7, updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+userColumns,
		id, username, displayName, NormalizeEmail(email), role, grants, enabled))
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
