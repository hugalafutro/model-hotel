package frontdesk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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
