-- Add per-key tokens-per-minute (TPM) cap to virtual_keys.
-- NULL = no TPM cap (or fall back to a global default if configured).
-- Enforced consumer-side: when a key exceeds its minute token budget its
-- next request is rejected with 429; the upstream provider is not throttled.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS rate_limit_tpm INTEGER;
