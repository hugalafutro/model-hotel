-- What a request cost in US dollars, priced at the figures the last model it
-- was dispatched to carried when the row was written. Stamped by the proxy on
-- the terminal write, so a later price change never rewrites history. NULL
-- when the request never reached a provider or the model held no input or
-- output price; a dispatched request that charged nothing, and a free model,
-- price to 0. Cache-hit prompt tokens take the cache-hit price when the model
-- has one, otherwise the input price; prompt tokens beyond the cache split (a
-- failover group's rejected earlier candidates) take the input price;
-- completion and reasoning tokens both take the output price, the same way
-- tokens_used meters them. The whole row is priced at one model even when a
-- walked group's members charge differently.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cost_usd DOUBLE PRECISION;

-- Rows written before this column existed are priced at today's figures, the
-- closest estimate on hand. The serving model is the failover group's
-- resolved member when there was one, else the requested model. Only rows
-- with no cost yet are touched, so a re-run prices nothing twice. The price
-- columns are REAL; widened first so the sum accumulates the way the proxy's
-- float64 arithmetic does. An old exhausted-group row stays NULL even when a
-- rejected candidate's prompt was charged onto it: exhaustion nulls
-- provider_id, so nothing says which member to price it at. The live path
-- keeps the last candidate and prices such a row; the estimate does not.
UPDATE request_logs rl SET cost_usd = (
      (CASE WHEN rl.tokens_prompt_cache_hit > 0 AND m.input_price_per_million_cache_hit IS NOT NULL
            THEN rl.tokens_prompt_cache_hit * m.input_price_per_million_cache_hit::double precision
               + (rl.tokens_prompt_cache_miss
                  + GREATEST(COALESCE(rl.tokens_prompt, 0) - rl.tokens_prompt_cache_hit - rl.tokens_prompt_cache_miss, 0))
                 * m.input_price_per_million::double precision
            ELSE COALESCE(rl.tokens_prompt, 0) * m.input_price_per_million::double precision END)
    + (COALESCE(rl.tokens_completion, 0) + rl.tokens_completion_reasoning) * m.output_price_per_million::double precision
  ) / 1000000.0
FROM models m
WHERE rl.cost_usd IS NULL
  AND m.provider_id = rl.provider_id
  AND m.model_id = COALESCE(NULLIF(rl.resolved_model_id, ''), rl.model_id)
  AND m.input_price_per_million IS NOT NULL
  AND m.output_price_per_million IS NOT NULL;
