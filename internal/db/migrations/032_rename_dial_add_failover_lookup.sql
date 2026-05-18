-- Rename safe_dial_ms to dial_ms (now measures full DNS+TCP dial, not just DNS).
ALTER TABLE request_logs RENAME COLUMN safe_dial_ms TO dial_ms;

-- Add failover_lookup_ms for hotel-model requests where the first lookup
-- is the failover group (not the model entity itself).
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS failover_lookup_ms DOUBLE PRECISION DEFAULT 0;
