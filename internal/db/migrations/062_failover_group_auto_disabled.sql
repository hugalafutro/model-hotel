-- Marks a failover group that DISCOVERY disabled (fewer than 2 routable members
-- left, so hotel/<model> routing for it is dead). NULL means "not disabled by
-- discovery", which covers both an enabled group and one the operator switched
-- off on purpose.
--
-- The column exists because group_enabled alone cannot answer "who did this".
-- Four paths write group_enabled = false with an identical bit: the auto-disable
-- in failover.revalidateCustomGroups, the operator's PUT /api/failover-groups/{id},
-- the dashboard's own cascade when disabling a member drops the group below two
-- routable entries, and the config-sync import mirroring the fleet primary. Only
-- the first is discovery's opinion. auto_created cannot stand in for this: the
-- auto-disable skips auto-created groups entirely, and sync either re-enables or
-- deletes them every scan, so every DURABLY disabled group is auto_created =
-- false — precisely the ambiguous set.
--
-- Without the distinction, a group the operator deliberately switched off would
-- surface as a claim on every poll and nag them about their own configuration
-- forever, which is the same failure mode models.disabled_manually exists to
-- prevent. A permanently non-zero badge destroys the point of counting at all.
--
-- Like models.discovery_dismissed_at, this is persisted INTENT, not a stored
-- claim: the claim itself stays derived (group_enabled = false AND
-- auto_disabled_at IS NOT NULL), so it auto-resolves the moment the group is
-- re-enabled and can never drift from live state. Every operator-driven write of
-- group_enabled clears it back to NULL, so a group that is auto-disabled,
-- re-enabled, then auto-disabled again reads as a fresh claim instead of
-- inheriting a stale stamp.
--
-- One-way door, worth knowing before touching any of this: the auto-disable
-- skips groups that are already disabled (revalidateCustomGroups), so a group
-- that is disabled without a stamp is never re-examined and never stamped. Two
-- consequences. (1) Groups disabled BEFORE this migration read as
-- operator-disabled forever; that is the safe direction (silent, not nagging),
-- and it is why there is no backfill — the journal is the only record of the old
-- provenance, and re-deriving it from routable counts would double-count
-- gone-model claims. (2) Any code path that clears this column without an
-- operator behind it destroys a live claim PERMANENTLY. Internal maintenance
-- must not go through failover.Repository.Update, which clears it by design; see
-- Repository.pruneMembership for the pattern to follow instead.
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS auto_disabled_at TIMESTAMPTZ;

-- Partial index: the claim query only ever looks at discovery-disabled groups.
CREATE INDEX IF NOT EXISTS idx_failover_groups_auto_disabled
    ON model_failover_groups (display_model)
    WHERE group_enabled = false AND auto_disabled_at IS NOT NULL;
