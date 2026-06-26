-- Records the last time the fleet-sync wizard converged the fleet, so the wizard
-- can tell the operator it has run before (and against which primary) instead of
-- reopening at step 1 with no history after a container rebuild. Single-row table
-- (id pinned to 1); last_run_at is INTEGER Unix nanoseconds to match the rest of
-- the schema. No row exists until the first successful sync.
CREATE TABLE IF NOT EXISTS fleet_sync_state (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    last_run_at  INTEGER NOT NULL,
    primary_id   TEXT NOT NULL,
    primary_name TEXT NOT NULL
);
