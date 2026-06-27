-- Front Desk outbound Apprise alerting: notify the fleet operator when a member
-- goes unreachable, a config sync fails, or a version read keeps failing. Mirrors
-- the main gateway's alerting (internal/alert) but over Front Desk's own event
-- bus and HA event catalog. Best-effort: a missing/failing apprise-api never
-- affects polling or syncing.
--
-- alert_enabled         : master on/off for outbound notifications.
-- alert_apprise_api_url : base URL of the operator's apprise-api container.
-- alert_apprise_targets : Apprise URL(s), encrypted at rest (auth.EncryptString,
--                         enc:v1: token) like member admin tokens; masked over the API.
-- alert_events          : CSV of enabled event Types (the per-event picker).
ALTER TABLE settings ADD COLUMN alert_enabled         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN alert_apprise_api_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN alert_apprise_targets TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN alert_events          TEXT    NOT NULL DEFAULT '';

-- Seed the picker with the default-on HA events so a fresh install shows a sane
-- selection. A later empty value means the operator deselected everything (and
-- nothing fires) -- the seed is what distinguishes first-run from "cleared all".
-- Keep this list in step with the DefaultOn entries in alerts.go (fdCatalog);
-- TestMigrationSeedMatchesCatalogDefaults guards against drift.
UPDATE settings SET alert_events = 'health.down,health.up,config.sync_failed,version.fetch_failed' WHERE id = 1;
