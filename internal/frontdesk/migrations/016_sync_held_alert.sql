-- Add the new default-on alert event config.sync_held to the picker seed. Like
-- migration 015, this only rewrites the CSV when it still equals the exact prior
-- (015) seed, i.e. the operator never touched the picker: a customized selection
-- (including a deliberately empty one) is left untouched, so this never clobbers
-- an operator's choices to force the new event on.
--
-- Keep the target list in step with the DefaultOn entries in alerts.go (fdCatalog);
-- TestMigrationSeedMatchesCatalogDefaults guards a fresh install's seed.
UPDATE settings
SET alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,config.sync_held,version.fetch_failed'
WHERE id = 1
  AND alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,version.fetch_failed';
