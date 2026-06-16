-- Durable log of changes detected by *background* model discovery (scheduled or
-- startup runs), so they can be surfaced as a count badge on the Models nav and
-- reviewed later. Manual discovery is shown to the user immediately in the
-- response modal and is NOT recorded here.
--
-- Each row is one provider's serialized DiscoveryDiff for a single run. Rows are
-- "unseen" until the user opens the viewer, which flips seen = true. The partial
-- index keeps the badge's unseen-count query cheap as the table grows.
CREATE TABLE IF NOT EXISTS discovery_changes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    detected_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    source        TEXT NOT NULL,            -- 'scheduled' | 'startup'
    provider_id   UUID,                     -- nullable: provider may be deleted later
    provider_name TEXT NOT NULL,
    diff          JSONB NOT NULL,           -- one provider's serialized DiscoveryDiff
    seen          BOOLEAN NOT NULL DEFAULT false
);

CREATE INDEX IF NOT EXISTS idx_discovery_changes_unseen
    ON discovery_changes (detected_at) WHERE NOT seen;
