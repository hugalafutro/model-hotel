-- Provider names have been unique on the raw string since migration 023, but
-- that is not the form the gateway routes on. provider.NormalizeName maps every
-- space to a hyphen, and the normalized name is what "<provider>/<model>"
-- routing ids carry, what the provider cache keys on, and what GetByName falls
-- back to when the raw lookup misses. So "a b" and "a-b" pass the raw
-- constraint, produce the same routing id, share one cache slot, and resolve to
-- whichever row the fallback query returns first.
--
-- The unique index closes that: names are unique in the normalized form too.
-- The raw constraint stays, because ON CONFLICT (name) in the config-sync
-- import keys on it.
--
-- An install that already holds a colliding pair refuses to start rather than
-- being repaired here. It is already misrouting, and either name is load
-- bearing: renaming one changes the routing id every client and every custom
-- failover group entry names it by, so the operator picks which one survives.
DO $$
DECLARE
    collisions text;
BEGIN
    SELECT string_agg(g.names, '; ' ORDER BY g.names) INTO collisions
    FROM (
        SELECT string_agg(quote_literal(name), ', ' ORDER BY name) AS names
        FROM providers
        GROUP BY REPLACE(name, ' ', '-')
        HAVING count(*) > 1
    ) g;

    IF collisions IS NOT NULL THEN
        -- The remedy belongs in the message, not only in the HINT: the Go driver
        -- renders a server error as severity, message and SQLSTATE alone, so a
        -- HINT reaches an operator running psql and nobody else.
        RAISE EXCEPTION 'provider names collide once spaces become hyphens, which is the form routing uses; rename one provider in each group so the names differ by more than a space, then restart. Groups: %', collisions
            USING HINT = 'rename one provider in each group so the names differ by more than a space, then restart';
    END IF;

    CREATE UNIQUE INDEX IF NOT EXISTS providers_name_normalized_unique
        ON providers ((REPLACE(name, ' ', '-')));
END
$$;
