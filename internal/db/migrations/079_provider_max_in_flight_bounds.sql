-- Bound the per-provider in-flight ceiling at rest, mirroring the rule every
-- write path applies (provider.ValidateMaxInFlight: NULL for no ceiling, else
-- 1..10000). The interactive API enforced it from the day the column arrived
-- (migration 077); the config-sync import path wrote the envelope's value
-- verbatim until the commit this migration accompanies, so a member that took
-- an envelope carrying -5 or 999999999 persisted it, exported it, and shipped
-- it to every other member on the next sync.
--
-- The runtime reads a ceiling of zero or less as "no ceiling"
-- (proxy.effectiveLimit), so a value below one never rejected traffic: it
-- silently deleted the operator's cap. Returning such a row to NULL is
-- therefore meaning-preserving in the only safe direction (NULL is the
-- documented "no ceiling"), and it removes the landmine before the import
-- path starts refusing whole envelopes over it, which is what would otherwise
-- stop a fleet converging over one stale row.
UPDATE providers SET max_in_flight = NULL WHERE max_in_flight IS NOT NULL AND (max_in_flight < 1 OR max_in_flight > 10000);

-- NULL passes: a CHECK that evaluates to NULL is not a violation, and the IS
-- NULL arm is spelled out so a reader does not have to know that.
--
-- Postgres has no ADD CONSTRAINT IF NOT EXISTS, hence the duplicate_object
-- swallow: migrations are recorded in schema_migrations and normally run once,
-- but a restore can replay one against a database that already has the
-- constraint.
DO $$
BEGIN
    ALTER TABLE providers ADD CONSTRAINT providers_max_in_flight_bounds CHECK (
        max_in_flight IS NULL OR (max_in_flight BETWEEN 1 AND 10000)
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
