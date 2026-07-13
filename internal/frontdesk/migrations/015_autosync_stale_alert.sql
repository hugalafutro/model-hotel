-- Add the new default-on alert event config.autosync_stale to the picker seed.
-- Migration 007 seeded alert_events with the original four default-on events, so
-- a fresh install runs this right after and lands on the full current default
-- set. On an existing install this only rewrites the CSV when it still equals the
-- exact 007 seed, i.e. the operator never touched the picker: a customized
-- selection (including a deliberately empty one) is left untouched, so this never
-- clobbers an operator's choices to force the new event on.
--
-- Keep the target list in step with the DefaultOn entries in alerts.go
-- (fdCatalog); TestMigrationSeedMatchesCatalogDefaults guards a fresh install's
-- seed against the catalog.
UPDATE settings
SET alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,version.fetch_failed'
WHERE id = 1
  AND alert_events = 'health.down,health.up,config.sync_failed,version.fetch_failed';
