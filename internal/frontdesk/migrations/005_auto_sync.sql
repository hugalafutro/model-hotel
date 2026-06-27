-- HA auto config sync: let Front Desk propagate the primary's config to the
-- fleet automatically when it changes, instead of only through the manual
-- wizard. The operator designates a primary and flips auto-sync on; the poller
-- then watches the primary's config hash and re-syncs replicas on a change.
--
-- auto_sync_enabled    : master on/off for the automatic propagation loop.
-- auto_sync_primary_id  : the designated source-of-truth member (empty = none).
-- auto_sync_last_hash   : the primary config hash last applied to the fleet, so
--                         the poller can tell "changed since last sync" cheaply.
ALTER TABLE settings ADD COLUMN auto_sync_enabled    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN auto_sync_primary_id TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN auto_sync_last_hash  TEXT    NOT NULL DEFAULT '';

-- Per-member record of the last config sync Front Desk applied to it (wizard or
-- automatic), powering the Members table "Last Config Sync" column and tooltip.
-- last_config_sync_at is INTEGER Unix nanoseconds (NULL until first sync); the
-- reason explains why it synced (e.g. the primary's config changed).
ALTER TABLE members ADD COLUMN last_config_sync_at     INTEGER;
ALTER TABLE members ADD COLUMN last_config_sync_reason TEXT NOT NULL DEFAULT '';
