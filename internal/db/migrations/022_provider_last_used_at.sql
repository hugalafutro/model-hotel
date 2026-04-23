-- Add last_used_at to providers
ALTER TABLE providers ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;

-- Backfill with the most recent request log timestamp per provider
UPDATE providers p
SET last_used_at = (
    SELECT MAX(created_at)
    FROM request_logs rl
    WHERE rl.provider_id = p.id
)
WHERE EXISTS (
    SELECT 1 FROM request_logs rl2 WHERE rl2.provider_id = p.id
);
