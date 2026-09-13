-- Where each stored price came from, so the dashboard can say so. Prices reach
-- a model row from four places (the provider's own listing, an embedded
-- catalog override, models.dev enrichment, or an operator edit) and nothing
-- recorded which; an operator reading a figure could not tell a vendor's own
-- number from a stale override. Keyed by price field: {"input": "...",
-- "cache_hit": "...", "output": "..."}, absent when that price is unset. The
-- upsert merges it key by key the way it merges the prices, so a scan that
-- keeps a stored price keeps its source too. Rows written before this column
-- carry no source until their next discovery scan.
ALTER TABLE models ADD COLUMN IF NOT EXISTS price_sources JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Rows the operator pinned before this column existed hold the operator's
-- own figures: the upsert keeps a pinned row's stored prices, so without a
-- source the next scan's label would land on numbers the scan did not write.
-- Stamp every price such a row holds as manual (a pin without an edit gets
-- the same label, the closest of the four). Unpinned rows stay unsourced
-- until their next scan writes prices and sources together.
UPDATE models SET price_sources =
       (CASE WHEN input_price_per_million IS NULL THEN '{}' ELSE '{"input":"manual"}' END)::jsonb
    || (CASE WHEN input_price_per_million_cache_hit IS NULL THEN '{}' ELSE '{"cache_hit":"manual"}' END)::jsonb
    || (CASE WHEN output_price_per_million IS NULL THEN '{}' ELSE '{"output":"manual"}' END)::jsonb
WHERE price_customized AND price_sources = '{}'::jsonb;
