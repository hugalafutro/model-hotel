-- One row per physical instance. Two adds of the same host under different
-- URLs could both pass the application-side dedup scan before either row
-- existed; the index refuses the second insert atomically, and the add maps
-- the violation to already_member. Rows are never deleted here: where an older
-- database already holds two rows with one instance id, the later rows' ids
-- are cleared (they read as "not yet learned" and the next verification
-- backfills or refuses them), so the index can be created.
UPDATE members SET instance_id = ''
WHERE instance_id != ''
  AND EXISTS (
    SELECT 1 FROM members o
    WHERE o.instance_id = members.instance_id
      AND (o.created_at < members.created_at
           OR (o.created_at = members.created_at AND o.id < members.id))
  );
CREATE UNIQUE INDEX IF NOT EXISTS members_instance_id_unique
  ON members(instance_id) WHERE instance_id != '';
