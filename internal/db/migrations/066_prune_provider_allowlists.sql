-- Deleting a provider used to leave its UUID behind in two TEXT[] allow-lists:
-- virtual_keys.allowed_providers (037) and users.allowed_providers (065). A
-- Postgres array cannot carry a foreign key, so there is no ON DELETE action to
-- lean on and nothing ever cleaned these up.
--
-- The stale entries were never a runtime hazard on their own: the proxy matches
-- candidates by provider id (proxy_request.go filters by c.provider.ID.String()),
-- so a dangling id simply matches nothing. They were a hazard to fleet
-- replication, because config-sync translates ids to provider NAMES on export
-- (exportVirtualKeys/exportUsers in internal/api/configsync_export.go) and an
-- unresolvable id silently vanishes from the envelope, which is how a restricted
-- key once round-tripped as unrestricted.
--
-- This migration repairs the rows an upgrading install already carries. Keeping
-- it true from here on is done in Go, at the two delete sites, by
-- provider.PruneAllowLists (internal/provider/allowlists.go). A database trigger
-- would also have covered raw SQL, but every model-hotel pg_dump would then
-- carry a FUNCTION and a TRIGGER, and the backup restore endpoint rejects any
-- dump containing either (checkDangerousObjects in
-- internal/api/backup_restore.go). Exempting them would have meant a
-- hand-maintained list of object names that a pg_restore TOC listing cannot
-- tell apart from a tampered dump reusing those same names, and verifying the
-- bodies would mean parsing SQL out of a custom-format dump. That is a
-- permanently wider security boundary than two known call sites are worth.
--
-- Why this cleanup is only safe now: pruning can empty an array, and until
-- recently a non-NULL empty allowed_providers read as "every provider allowed"
-- at the proxy. Emptying an array would have been an escalation. The proxy now
-- treats any non-NULL list as "exactly these members, including none of them"
-- (effectiveAllowedProviders in internal/proxy/proxy_request.go returns a
-- non-nil list unchanged, and the caller 403s when the filtered candidate set is
-- empty), so an emptied array denies everything, which is exactly what a key or
-- account scoped solely to deleted providers should do. NULL remains the only
-- value meaning unrestricted.
--
-- Note the deliberate contrast with migration 065, which converted legacy empty
-- arrays TO NULL. That ran against rows written while empty meant allow-all, so
-- NULL preserved their behaviour. This migration creates empty arrays under the
-- new meaning, where empty denies. Same representation, opposite eras; both
-- correct for the code shipping alongside them.
--
-- Neither table constrains this column: the only CHECKs on virtual_keys and
-- users are the rate-limit bounds added in 064 (plus users_role_check), so '{}'
-- is accepted at rest. The API write paths reject an empty array
-- (internal/api/virtualkeys.go, internal/api/users.go) because an operator
-- typing "restrict to nothing" is far more likely a mistake than an intent;
-- that is a UI guard, not a storage invariant, and it does not apply here.

-- Rewrites only rows that carry a restriction; NULL (unrestricted) rows are left
-- alone. A row whose ids are all dangling becomes '{}' and therefore denies
-- everything, which matches what it already did at runtime, so no live
-- behaviour changes. Both statements are idempotent: a second run finds nothing
-- dangling left and writes each surviving list back unchanged.
UPDATE virtual_keys vk
   SET allowed_providers = COALESCE((
           SELECT array_agg(elem)
             FROM unnest(vk.allowed_providers) AS elem
            WHERE elem IN (SELECT id::text FROM providers)
       ), '{}'::text[])
 WHERE vk.allowed_providers IS NOT NULL;

UPDATE users u
   SET allowed_providers = COALESCE((
           SELECT array_agg(elem)
             FROM unnest(u.allowed_providers) AS elem
            WHERE elem IN (SELECT id::text FROM providers)
       ), '{}'::text[])
 WHERE u.allowed_providers IS NOT NULL;
