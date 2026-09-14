-- A user's dollar budget (087) sums request_logs.owner_user_id alone, so
-- that reassigning or deleting a key leaves what was spent with whoever
-- spent it. The proxy now stamps the owner on keyed rows as well as keyless
-- ones (the log views keep resolving a keyed row through the key's current
-- owner). Two consequences for the table, kept out of 087 so its constraint
-- locks on virtual_keys and users are not held for the length of this pass:
--
-- 1. Stamp the current owner onto keyed history once, so the first budget
--    period after the upgrade is not undercounted. The table is bounded by
--    log retention; this runs in its own transaction and touches only rows
--    whose key has an owner and that carry none yet.
UPDATE request_logs rl
SET owner_user_id = vk.owner_user_id
FROM virtual_keys vk
WHERE rl.virtual_key_id = vk.id
  AND rl.owner_user_id IS NULL
  AND vk.owner_user_id IS NOT NULL;

-- 2. The spend sums are range reads (one subject, this period). The two
--    single-column partial indexes from 067 and 074 answered a membership
--    test; each becomes a (subject, created_at) index so a sum reads the
--    period's rows and no more. Same partial predicates, same writers.
DROP INDEX IF EXISTS idx_request_logs_owner;
CREATE INDEX IF NOT EXISTS idx_request_logs_owner_created
    ON request_logs (owner_user_id, created_at) WHERE owner_user_id IS NOT NULL;
DROP INDEX IF EXISTS idx_request_logs_virtual_key_id;
CREATE INDEX IF NOT EXISTS idx_request_logs_virtual_key_created
    ON request_logs (virtual_key_id, created_at) WHERE virtual_key_id IS NOT NULL;
