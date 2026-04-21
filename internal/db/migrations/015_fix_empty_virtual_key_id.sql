-- Fix empty string virtual_key_id values stored instead of NULL for admin/test-path logs.
-- An empty string UUID is not valid and causes false "deleted" detection.
UPDATE request_logs SET virtual_key_id = NULL WHERE virtual_key_id IS NOT NULL AND virtual_key_id::text = '';