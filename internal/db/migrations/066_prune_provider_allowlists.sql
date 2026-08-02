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
-- Why this is only safe to do now: pruning can empty an array, and until
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

-- One-time cleanup of ids that are already dangling. Rewrites only rows that
-- carry a restriction; NULL (unrestricted) rows are left alone. A row whose ids
-- are all dangling becomes '{}' and therefore denies everything, which matches
-- what it already did at runtime, so no live behaviour changes.
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

-- Keep it true from here on.
--
-- THIS IS THE ONLY TRIGGER IN THE SCHEMA. Nothing in the Go code says these two
-- UPDATEs happen, so a reader of provider.Repository.Delete or of the
-- config-sync declarative replace will not see them. A trigger is used anyway
-- because it is the only mechanism that is atomic with the delete by
-- construction and that covers every path: the admin delete
-- (internal/provider/provider.go), the config-sync bulk delete
-- (`DELETE FROM providers WHERE name <> ALL($1)` in
-- internal/api/configsync_apply.go, which runs inside the import transaction),
-- any future delete path, and manual SQL. Go-side pruning at the two known call
-- sites would shrink the problem rather than remove it.
--
-- AFTER DELETE so the provider row is already gone; the return value of an
-- AFTER FOR EACH ROW trigger is ignored, hence NULL. FOR EACH ROW rather than a
-- statement-level trigger so the bulk delete prunes every removed provider, not
-- just one; provider counts are small, so the per-row cost is irrelevant.
--
-- array_remove on a NULL array yields NULL, and the WHERE clauses skip NULL rows
-- anyway (NULL @> ARRAY[...] is NULL, not true), so an unrestricted key or
-- account is never converted into a restricted one. Pruning the last element
-- yields '{}', not NULL, which is the whole point: the row stays restricted.
CREATE OR REPLACE FUNCTION prune_provider_from_allowlists() RETURNS TRIGGER AS $$
BEGIN
    UPDATE virtual_keys
       SET allowed_providers = array_remove(allowed_providers, OLD.id::text)
     WHERE allowed_providers @> ARRAY[OLD.id::text];

    UPDATE users
       SET allowed_providers = array_remove(allowed_providers, OLD.id::text)
     WHERE allowed_providers @> ARRAY[OLD.id::text];

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS providers_prune_allowlists ON providers;
CREATE TRIGGER providers_prune_allowlists
    AFTER DELETE ON providers
    FOR EACH ROW
    EXECUTE FUNCTION prune_provider_from_allowlists();
