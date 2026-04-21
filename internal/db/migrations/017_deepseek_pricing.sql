-- DeepSeek pricing support: cache-aware token tracking and per-model price columns

-- Add cache hit/miss tracking columns to request_logs
-- Default 0 ensures backward compatibility with old log entries
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_prompt_cache_hit INT NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_prompt_cache_miss INT NOT NULL DEFAULT 0;

-- Add separate price columns for cache hit vs miss to models table
-- input_price_per_million already exists (represents cache miss price for DeepSeek)
-- Add separate column for cache hit price
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_price_per_million_cache_hit REAL;
