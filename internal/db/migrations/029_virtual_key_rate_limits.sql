-- Add per-key rate limit columns to virtual_keys.
-- Nullable: when NULL, the key falls back to global rate_limit_rps / rate_limit_burst settings.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS rate_limit_rps DOUBLE PRECISION DEFAULT NULL;
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS rate_limit_burst INTEGER DEFAULT NULL;
