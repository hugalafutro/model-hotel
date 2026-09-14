-- A listing stating "NaN", "Inf" or a negative price was stored as a real
-- price until the parsers refused such figures, and rediscovery keeps a stored
-- price when the listing reports unknown, so a row that once took one keeps
-- it. Such a price prices nothing: it becomes NULL (unknown), and a request
-- cost derived from one becomes NULL too. Postgres orders NaN above every
-- number, so NaN is named rather than caught by the comparisons. Idempotent.
UPDATE models SET input_price_per_million = NULL
    WHERE input_price_per_million < 0 OR input_price_per_million = 'NaN'::double precision OR input_price_per_million = 'Infinity'::double precision;
UPDATE models SET output_price_per_million = NULL
    WHERE output_price_per_million < 0 OR output_price_per_million = 'NaN'::double precision OR output_price_per_million = 'Infinity'::double precision;
UPDATE models SET input_price_per_million_cache_hit = NULL
    WHERE input_price_per_million_cache_hit < 0 OR input_price_per_million_cache_hit = 'NaN'::double precision OR input_price_per_million_cache_hit = 'Infinity'::double precision;
UPDATE request_logs SET cost_usd = NULL
    WHERE cost_usd < 0 OR cost_usd = 'NaN'::double precision OR cost_usd = 'Infinity'::double precision;
