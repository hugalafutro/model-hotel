-- Rerank models bill per search unit (one query against up to 100 documents),
-- not per token, so the per-million price columns cannot price them. This is
-- the price of a thousand search units in US dollars, a pointer like the other
-- price columns: NULL means unknown, and price_customized pins it the same way.
ALTER TABLE models ADD COLUMN IF NOT EXISTS search_price_per_thousand REAL;

-- How many search units the provider billed a rerank request for, read off
-- the answer's meta.billed_units.search_units. Zero on every other row and on
-- rerank rows written before this column existed; those old rows stay unpriced.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS search_units INTEGER NOT NULL DEFAULT 0;
