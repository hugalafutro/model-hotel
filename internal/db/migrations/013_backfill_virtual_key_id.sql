-- Backfill virtual_key_id for existing logs that have virtual_key_name set but no virtual_key_id.
-- Only match if the VK existed when the log was created (rl.created_at >= vk.created_at),
-- so we don't incorrectly assign a newly-created same-named key to old logs from a deleted key.
UPDATE request_logs rl
SET virtual_key_id = vk.id
FROM virtual_keys vk
WHERE rl.virtual_key_name = vk.name
  AND rl.virtual_key_id IS NULL
  AND rl.virtual_key_name IS NOT NULL
  AND rl.virtual_key_name != ''
  AND rl.created_at >= vk.created_at;