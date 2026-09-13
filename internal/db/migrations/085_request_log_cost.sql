-- What a request cost in US dollars, priced at the figures its serving model
-- carried when the request completed. Stamped by the proxy on the terminal
-- write, so a later price change never rewrites history. NULL when the request
-- never reached a provider or the model held no input or output price; a free
-- model prices to 0. Cache-hit prompt tokens take the cache-hit price when the
-- model has one, otherwise the input price; completion and reasoning tokens
-- both take the output price, the same way tokens_used meters them.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cost_usd DOUBLE PRECISION;

-- Rows written before this column existed are priced at today's figures, the
-- closest estimate on hand. The serving model is the failover group's
-- resolved member when there was one, else the requested model. Only rows
-- with no cost yet are touched, so a re-run prices nothing twice.
UPDATE request_logs rl SET cost_usd = (
      (CASE WHEN rl.tokens_prompt_cache_hit > 0 AND m.input_price_per_million_cache_hit IS NOT NULL
            THEN rl.tokens_prompt_cache_hit * m.input_price_per_million_cache_hit
               + rl.tokens_prompt_cache_miss * m.input_price_per_million
            ELSE COALESCE(rl.tokens_prompt, 0) * m.input_price_per_million END)
    + (COALESCE(rl.tokens_completion, 0) + rl.tokens_completion_reasoning) * m.output_price_per_million
  ) / 1000000.0
FROM models m
WHERE rl.cost_usd IS NULL
  AND m.provider_id = rl.provider_id
  AND m.model_id = COALESCE(NULLIF(rl.resolved_model_id, ''), rl.model_id)
  AND m.input_price_per_million IS NOT NULL
  AND m.output_price_per_million IS NOT NULL;
