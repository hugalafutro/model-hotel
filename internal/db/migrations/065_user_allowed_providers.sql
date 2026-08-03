-- Per-user provider entitlement. NULL = no cap (every provider), matching the
-- per-key column added in 037. Existing users are grandfathered to NULL so no
-- deployment changes behaviour on upgrade.
ALTER TABLE users ADD COLUMN IF NOT EXISTS allowed_providers TEXT[] DEFAULT NULL;

-- Normalise the legacy ambiguous value before the runtime semantics change.
-- A non-NULL empty array currently reads as "unrestricted" at the proxy and
-- will read as "restricted to nothing" afterwards. No current write path
-- produces one; this protects any row predating the API's empty-array
-- rejection, so its effective behaviour is unchanged.
UPDATE virtual_keys SET allowed_providers = NULL
 WHERE allowed_providers IS NOT NULL AND cardinality(allowed_providers) = 0;
