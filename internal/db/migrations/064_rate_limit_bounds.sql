-- Normalize per-key and per-user rate limits that are out of bounds, then make
-- the bounds true at rest.
--
-- The interactive API has enforced rps >= 0, burst >= 1 and tpm >= 1
-- (validateRateLimits, internal/api/virtualkeys.go) since PR #226 — but the
-- per-key rps/burst columns shipped in PR #1 with the looser check, so an
-- install from that window can hold a virtual key with burst = 0, and nothing
-- ever cleaned those rows up. A restored old backup or a hand-edited row lands
-- in the same state.
--
-- That legacy row is now a fleet-wide hazard rather than a local curiosity: the
-- config-sync import path validates the same bounds and refuses the ENTIRE
-- envelope when one fails, so a single stale row on the primary stops every
-- member converging, with nothing but a log line to say why. Refusing is the
-- right call for a poisoned envelope, so the fix belongs here — remove the
-- landmine instead of softening the guard.
--
-- NULL is the documented "fall back to the global setting" value for the per-key
-- columns (see migration 029) and "no cap" for the per-user ones (051), so
-- nulling an out-of-bounds value is meaning-preserving in the only direction
-- that is safe: it hands the row back to the operator's configured default
-- rather than inventing a limit. It cannot be a downgrade either, because every
-- value it touches is one the runtime already refused to enforce sanely — a
-- burst < 1 blocked the key outright and a tpm < 1 metered nothing at all.
UPDATE virtual_keys SET rate_limit_rps   = NULL WHERE rate_limit_rps   < 0;
UPDATE virtual_keys SET rate_limit_burst = NULL WHERE rate_limit_burst < 1;
UPDATE virtual_keys SET rate_limit_tpm   = NULL WHERE rate_limit_tpm   < 1;

UPDATE users SET rate_limit_rps   = NULL WHERE rate_limit_rps   < 0;
UPDATE users SET rate_limit_burst = NULL WHERE rate_limit_burst < 1;
UPDATE users SET rate_limit_tpm   = NULL WHERE rate_limit_tpm   < 1;

-- With the historical rows cleaned, hold the line in the schema. Every writer of
-- these columns already validates (virtualkey.Create/Update and user.Create/Update
-- behind validateRateLimits, config-sync behind validateSyncedRateLimits), so
-- this constraint should never fire; it exists so the next writer cannot
-- reintroduce the state that made a whole fleet stop syncing, and so a
-- pg_restore of a pre-#226 dump is normalized by this migration on the way back
-- up rather than silently reseeding it.
--
-- NULL passes: a CHECK that evaluates to NULL is not a violation, and the
-- IS NULL arms are spelled out so a reader does not have to know that.
--
-- Postgres has no ADD CONSTRAINT IF NOT EXISTS, hence the duplicate_object
-- swallow: migrations are recorded in schema_migrations and normally run once,
-- but a restore can replay one against a database that already has the
-- constraint.
DO $$
BEGIN
    ALTER TABLE virtual_keys ADD CONSTRAINT virtual_keys_rate_limit_bounds CHECK (
        (rate_limit_rps   IS NULL OR rate_limit_rps   >= 0) AND
        (rate_limit_burst IS NULL OR rate_limit_burst >= 1) AND
        (rate_limit_tpm   IS NULL OR rate_limit_tpm   >= 1)
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE users ADD CONSTRAINT users_rate_limit_bounds CHECK (
        (rate_limit_rps   IS NULL OR rate_limit_rps   >= 0) AND
        (rate_limit_burst IS NULL OR rate_limit_burst >= 1) AND
        (rate_limit_tpm   IS NULL OR rate_limit_tpm   >= 1)
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
