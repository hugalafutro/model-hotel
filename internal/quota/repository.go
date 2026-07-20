// Package quota persists cached provider quota/usage snapshots so the
// dashboard can show fresh numbers on load without an upstream call in the
// request path. See migration 059_provider_quota_snapshots.sql.
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Snapshot is one cached provider quota/usage payload.
type Snapshot struct {
	ProviderID    uuid.UUID
	Kind          string // usage | balance | account
	Payload       json.RawMessage
	HTTPStatus    int
	FetchedAt     time.Time
	Source        string // poll | manual | fleet
	LastError     string
	LastAttemptAt *time.Time
}

// Repository persists provider_quota_snapshots rows.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a Repository backed by pool.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Upsert writes a fresh snapshot (used by poll and manual refresh), replacing
// any prior row for the provider+kind and clearing any recorded failure.
func (r *Repository) Upsert(ctx context.Context, s Snapshot) error {
	if s.FetchedAt.IsZero() {
		s.FetchedAt = time.Now()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO provider_quota_snapshots
			(provider_id, kind, payload, http_status, fetched_at, source, last_error, last_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULL, $5)
		ON CONFLICT (provider_id, kind) DO UPDATE SET
			payload         = EXCLUDED.payload,
			http_status     = EXCLUDED.http_status,
			fetched_at      = EXCLUDED.fetched_at,
			source          = EXCLUDED.source,
			last_error      = NULL,
			last_attempt_at = EXCLUDED.fetched_at`,
		s.ProviderID, s.Kind, s.Payload, s.HTTPStatus, s.FetchedAt, s.Source)
	return err
}

// Get returns the snapshot for provider+kind, or (nil, nil) when none exists.
func (r *Repository) Get(ctx context.Context, providerID uuid.UUID, kind string) (*Snapshot, error) {
	var s Snapshot
	var lastErr *string
	err := r.pool.QueryRow(ctx, `
		SELECT provider_id, kind, payload, http_status, fetched_at, source, last_error, last_attempt_at
		FROM provider_quota_snapshots WHERE provider_id = $1 AND kind = $2`,
		providerID, kind).Scan(
		&s.ProviderID, &s.Kind, &s.Payload, &s.HTTPStatus, &s.FetchedAt, &s.Source, &lastErr, &s.LastAttemptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastErr != nil {
		s.LastError = *lastErr
	}
	return &s, nil
}

// RecordFailure marks a failed refresh without discarding the last good
// payload (or creates a placeholder row if none exists yet).
func (r *Repository) RecordFailure(ctx context.Context, providerID uuid.UUID, kind, errMsg string) error {
	now := time.Now()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO provider_quota_snapshots (provider_id, kind, http_status, source, last_error, last_attempt_at, fetched_at)
		VALUES ($1, $2, 0, 'poll', $3, $4, $4)
		ON CONFLICT (provider_id, kind) DO UPDATE SET
			last_error = EXCLUDED.last_error,
			last_attempt_at = EXCLUDED.last_attempt_at`,
		providerID, kind, errMsg, now)
	return err
}
