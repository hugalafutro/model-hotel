-- Add the new default-on alert event config.sync_incomplete to the picker seed.
-- Like migrations 015/016/017, this only rewrites the CSV when it still equals
-- the exact prior (017) seed, i.e. the operator never touched the picker: a
-- customized selection (including a deliberately empty one) is left untouched, so
-- this never clobbers an operator's choices to force the new event on.
--
-- config.sync_incomplete sits directly after config.sync_held in fdCatalog order,
-- so it is spliced in there rather than appended, keeping the CSV in catalog order
-- and equal to DefaultEnabledCSVFor(fdCatalog).
--
-- Keep the target list in step with the DefaultOn entries in alerts.go (fdCatalog);
-- TestMigrationSeedMatchesCatalogDefaults guards a fresh install's seed.
UPDATE settings
SET alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,config.sync_held,config.sync_incomplete,version.fetch_failed,fleet.state_changed'
WHERE id = 1
  AND alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,config.sync_held,version.fetch_failed,fleet.state_changed';
