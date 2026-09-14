-- Migration 085 priced completion and reasoning tokens as two output charges.
-- Providers report reasoning inside completion_tokens (the
-- completion_tokens_details.reasoning_tokens figure is a breakdown, not an
-- extra count), so every reasoning request was charged for its thinking
-- twice, in the backfill and on every row stamped since. Reprice those rows
-- at their serving model's current prices, the same estimate 085 made, with
-- the prompt priced exactly as the proxy prices it now. Rows the proxy priced
-- exactly at the time are re-estimated too: a day of price drift is smaller
-- than the reasoning term being removed.
UPDATE request_logs rl SET cost_usd = (
      (CASE WHEN rl.tokens_prompt_cache_hit > 0 AND m.input_price_per_million_cache_hit IS NOT NULL
            THEN rl.tokens_prompt_cache_hit * m.input_price_per_million_cache_hit::double precision
               + (rl.tokens_prompt_cache_miss
                  + GREATEST(COALESCE(rl.tokens_prompt, 0) - rl.tokens_prompt_cache_hit - rl.tokens_prompt_cache_miss, 0))
                 * m.input_price_per_million::double precision
            ELSE COALESCE(rl.tokens_prompt, 0) * m.input_price_per_million::double precision END)
    + COALESCE(rl.tokens_completion, 0) * m.output_price_per_million::double precision
  ) / 1000000.0
FROM models m
WHERE rl.cost_usd IS NOT NULL
  AND rl.tokens_completion_reasoning > 0
  AND rl.state IN ('completed', 'failed')
  AND m.provider_id = rl.provider_id
  AND m.model_id = COALESCE(NULLIF(rl.resolved_model_id, ''), rl.model_id)
  AND m.input_price_per_million IS NOT NULL
  AND m.output_price_per_million IS NOT NULL;

-- A reasoning row whose model or provider is gone, or unpriced today, cannot
-- be repriced; its doubled figure becomes NULL (unknown) rather than standing.
UPDATE request_logs rl SET cost_usd = NULL
WHERE rl.cost_usd IS NOT NULL
  AND rl.tokens_completion_reasoning > 0
  AND rl.state IN ('completed', 'failed')
  AND NOT EXISTS (
    SELECT 1 FROM models m
    WHERE m.provider_id = rl.provider_id
      AND m.model_id = COALESCE(NULLIF(rl.resolved_model_id, ''), rl.model_id)
      AND m.input_price_per_million IS NOT NULL
      AND m.output_price_per_million IS NOT NULL);

-- A row still pending or streaming carries no usage yet; the proxy now leaves
-- its cost NULL until the terminal write, and stale cleanup, which rewrites
-- only the state, would otherwise have kept a zero stamped mid-stream.
UPDATE request_logs SET cost_usd = NULL
WHERE cost_usd IS NOT NULL AND state NOT IN ('completed', 'failed');
