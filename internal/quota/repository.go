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
	s.FetchedAt = sanitizeFetchedAt(s.FetchedAt)
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

// List returns every stored snapshot. The fleet export endpoint reads this on
// the primary so Front Desk can distribute the primary's snapshots to members.
func (r *Repository) List(ctx context.Context) ([]Snapshot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT provider_id, kind, payload, http_status, fetched_at, source, last_error, last_attempt_at
		FROM provider_quota_snapshots`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Snapshot
	for rows.Next() {
		var s Snapshot
		var lastErr *string
		if err := rows.Scan(&s.ProviderID, &s.Kind, &s.Payload, &s.HTTPStatus, &s.FetchedAt, &s.Source, &lastErr, &s.LastAttemptAt); err != nil {
			return nil, err
		}
		if lastErr != nil {
			s.LastError = *lastErr
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// sanitizeFetchedAt normalises a snapshot's fetch time. Unset means "now"; a
// time in the future is clamped to now.
//
// The clamp is load-bearing, not tidiness. FetchedAt crosses the wire on the
// fleet distribution path, and a future value broke two mechanisms at once:
// every staleness check measures with time.Since, which returns a NEGATIVE
// duration for a future stamp and so satisfies any "still fresh" comparison
// forever; and the upsert below keeps whichever row is newer, so the poisoned
// row outranked every genuine poll that followed. One bad timestamp froze a
// provider's quota data permanently, and near-silently.
//
// Clamped rather than rejected because the snapshot itself is still worth
// recording: only its claim about when it was fetched is impossible, and a
// fetch cannot have happened in the future. Same shape as the fix applied to
// the fleet rate-limit divisor, which had this bug in its own staleness check.
func sanitizeFetchedAt(t time.Time) time.Time {
	now := time.Now()
	if t.IsZero() || t.After(now) {
		return now
	}
	return t
}

// UpsertIfNewer writes only when there is no existing row or the incoming
// fetched_at is strictly newer, so an older fleet write never clobbers a
// member's fresher manual refresh. Returns whether the write applied.
//
// It persists the incoming failure marker rather than forcing it to NULL, which
// Upsert still does. The two differ because they mean different things: Upsert
// is a successful local fetch, and clearing on one is what stops the marker
// wedging a quota pin permanently. UpsertIfNewer is an import of somebody
// else's reading, and dropping the marker there would make a snapshot whose
// latest refresh failed look, to the receiving node, like affirmative proof the
// provider recovered.
//
// The second WHERE branch closes the gap the strictly-newer rule leaves: a
// failed refresh does not advance fetched_at (RecordFailure preserves it), so a
// node that already holds that exact snapshot would otherwise never learn the
// refresh behind it started failing. That branch is one-way — it fires only to
// add a marker the row does not have — so it can only ever hold a pin longer,
// never release one, and it is idempotent once applied. Clearing a marker still
// requires a strictly newer row, i.e. an actual successful refresh.
func (r *Repository) UpsertIfNewer(ctx context.Context, s Snapshot) (bool, error) {
	s.FetchedAt = sanitizeFetchedAt(s.FetchedAt)
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO provider_quota_snapshots
			(provider_id, kind, payload, http_status, fetched_at, source, last_error, last_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7::text, ''), $5)
		ON CONFLICT (provider_id, kind) DO UPDATE SET
			payload         = EXCLUDED.payload,
			http_status     = EXCLUDED.http_status,
			fetched_at      = EXCLUDED.fetched_at,
			source          = EXCLUDED.source,
			last_error      = EXCLUDED.last_error,
			last_attempt_at = EXCLUDED.fetched_at
		WHERE provider_quota_snapshots.fetched_at < EXCLUDED.fetched_at
		   OR (provider_quota_snapshots.fetched_at = EXCLUDED.fetched_at
		       AND provider_quota_snapshots.last_error IS NULL
		       AND EXCLUDED.last_error IS NOT NULL)`,
		s.ProviderID, s.Kind, s.Payload, s.HTTPStatus, s.FetchedAt, s.Source, s.LastError)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
