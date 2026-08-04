-- Drop the fleet-wide convergence marker (added in migration 005).
--
-- It has no reader. The auto-sync loop runs a pass on every settled tick rather
-- than skipping one whose primary hash matches a stored value, because config
-- drifts on a member as well as on the primary. Convergence is measured and
-- recorded per member instead: each member's own /api/config/version hash against
-- the primary's, surfaced on the Members table.
--
-- auto_sync_gen stays. It is the rearm fence and guards mutations, not this.
ALTER TABLE settings DROP COLUMN auto_sync_last_hash;
