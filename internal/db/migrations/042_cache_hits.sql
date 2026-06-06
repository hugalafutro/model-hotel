-- Add cache_hits JSONB column to request_logs for tracking cache hit/miss
-- status per overhead component at request time.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cache_hits jsonb;
