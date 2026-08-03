-- Owner attribution for request_logs rows that have no virtual key.
--
-- Non-admin log and stats views scope rows by joining request_logs.virtual_key_id
-- to virtual_keys.owner_user_id. Dashboard chat (/api/chat/chat, /arena,
-- /completions) authenticates a session rather than a key, so those rows insert
-- with virtual_key_id NULL and can never satisfy that join: a non-admin saw
-- their own /v1 traffic but none of their own chat activity.
--
-- This column is written ONLY for keyless rows (internal/proxy/logging.go
-- insertRequestLogAsync stores it when virtual_key_id would be NULL, and leaves
-- it NULL otherwise). Keyed traffic deliberately keeps resolving through the
-- key's CURRENT owner, because reassigning a key is meant to move its whole log
-- history with it; a denormalized column would instead freeze ownership at
-- request time. Those are different semantics, so the scope predicates became a
-- disjunction over the two disjoint row shapes rather than switching everything
-- to the column. A chat row has no key to reassign, so request-time attribution
-- is the only meaning available for it and the two can never disagree.
--
-- ON DELETE SET NULL matches virtual_keys.owner_user_id (migration 051):
-- deleting an account orphans its history rather than deleting it, and an
-- orphaned row falls back to admin-only visibility.
--
-- No backfill: rows written before this column predate any stored owner and
-- there is nothing reliable to recover one from, so they stay NULL and stay
-- admin-only, which is where they have always been.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

-- Partial, mirroring idx_virtual_keys_owner (051): only the keyless minority of
-- rows ever carries a value, so indexing the NULLs would be pure overhead on
-- the busiest table in the schema. Note request_logs.virtual_key_id itself is
-- deliberately unindexed, so the other half of the scope disjunction is not
-- index-backed either.
CREATE INDEX IF NOT EXISTS idx_request_logs_owner
    ON request_logs (owner_user_id) WHERE owner_user_id IS NOT NULL;
