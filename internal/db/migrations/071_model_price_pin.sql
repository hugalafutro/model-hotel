-- price_customized is the operator's pin on a model's prices: set when an
-- operator edits any price via the update API, cleared by an explicit unpin
-- (which also nulls the price columns so the next scan re-derives them).
-- While set, discovery upserts keep the stored prices untouched. While clear,
-- prices FOLLOW their source on every scan (live API, embedded catalog, or
-- models.dev enrichment, in that precedence), so vendor price changes and
-- corrected enrichment data propagate to existing rows instead of the old
-- fill-only behavior freezing whatever value landed first.
ALTER TABLE models ADD COLUMN IF NOT EXISTS price_customized BOOLEAN NOT NULL DEFAULT false;
