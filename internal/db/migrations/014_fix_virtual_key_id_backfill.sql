-- Fix incorrect virtual_key_id backfill: if the VK was created AFTER the log entry,
-- that VK couldn't have made the request, so clear the FK reference.
-- This handles the case where a deleted key's logs were incorrectly linked to a
-- newly-created key with the same name.
UPDATE request_logs rl
SET virtual_key_id = NULL
FROM virtual_keys vk
WHERE rl.virtual_key_id = vk.id
  AND rl.virtual_key_name = vk.name
  AND rl.created_at < vk.created_at;