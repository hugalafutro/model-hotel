-- Add the new default-on alert event backup.stale to the picker seed.
-- Like migrations 015/016/017/018, this only rewrites the CSV when it still
-- equals the exact prior (018) seed, i.e. the operator never touched the picker:
-- a customized selection (including a deliberately empty one) is left untouched,
-- so this never clobbers an operator's choices to force the new event on.
--
-- backup.stale is the last DefaultOn entry in fdCatalog order, so it is appended
-- rather than spliced, keeping the CSV in catalog order and equal to
-- DefaultEnabledCSVFor(fdCatalog). backup.recovered is DefaultOn:false and is
-- deliberately absent.
--
-- Keep the target list in step with the DefaultOn entries in alerts.go (fdCatalog);
-- TestMigrationSeedMatchesCatalogDefaults guards a fresh install's seed.
UPDATE settings
SET alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,config.sync_held,config.sync_incomplete,version.fetch_failed,fleet.state_changed,backup.stale'
WHERE id = 1
  AND alert_events = 'health.down,health.up,config.sync_failed,config.autosync_stale,config.sync_held,config.sync_incomplete,version.fetch_failed,fleet.state_changed';
