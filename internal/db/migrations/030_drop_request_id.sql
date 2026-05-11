-- Remove request_id column from request_logs.
-- This column was never populated by the proxy layer (always empty/null)
-- and served no purpose. The row UUID (id) uniquely identifies each request.
ALTER TABLE request_logs DROP COLUMN IF EXISTS request_id;