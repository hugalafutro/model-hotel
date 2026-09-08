package frontdesk

import (
	"context"
	"fmt"
)

// SetAutoSync persists an auto-sync choice (enabled + designated primary) and
// bumps the rearm generation in the same write, without the repoint and
// fleet-size guards SetAutoSyncGuarded enforces. It exists only to arrange a
// starting state in tests: production writes go through SetAutoSyncGuarded, and
// keeping this out of the shipped binary is what stops the guards it bypasses
// from being reintroduced through it.
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
