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

// GetFleetSyncState returns the recorded last-run marker. found means exactly
// "a real sync run is recorded", which is what every caller reads it as (the
// staleness watchdog and fleet state, the last-sync endpoint's 204, and quota
// distribution's setup gate). A row written by a no-run marker write
// (lonePrimaryMarker) names a primary without a run: its PrimaryID and
// PrimaryName are returned with found false and a zero LastRunAt.
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
	if at == 0 {
		return state, false, nil
	}
	state.LastRunAt = time.Unix(0, at).UTC()
	return state, true, nil
}

// noRunMarkerUpsert is the conflict tail of every no-run marker write: the
// marker names a primary without claiming a sync ran (last_run_at 0, which
// GetFleetSyncState reports as no run recorded), and a row that already names
// the same member is left alone, recorded run time included.
const noRunMarkerUpsert = ` ON CONFLICT(id) DO UPDATE SET last_run_at = 0,
		   primary_id = excluded.primary_id, primary_name = excluded.primary_name
		 WHERE primary_id <> excluded.primary_id`

// lonePrimaryMarker records, inside the transaction that inserted newID, the
// fleet's former lone member as the no-run marker when that insert made the
// roster two. On one member effectivePrimaryID names the sole member with
// nothing stored behind it, so without the record the two-member roster would
// resolve to nobody: the former lone member would be announced as a managed
// member, and the config it held while alone would be overwritten unannounced
// if the wizard then picked the newcomer. The marker keeps the resolver naming
// it, and the wizard preselecting it, until the operator designates a primary
// (SetAutoSyncGuarded then clears it). Sharing the insert's transaction means
// concurrent adds serialize on it: exactly one of them sees the roster at two.
const lonePrimaryMarker = `INSERT INTO fleet_sync_state (id, last_run_at, primary_id, primary_name)
		 SELECT 1, 0, id, name FROM members WHERE id <> ? AND (SELECT COUNT(*) FROM members) = 2` + noRunMarkerUpsert

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
