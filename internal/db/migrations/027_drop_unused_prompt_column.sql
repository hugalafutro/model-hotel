-- Drop the unused prompt column from request_logs.
-- Added in migration 006 but never written to by any application code.
ALTER TABLE request_logs DROP COLUMN IF EXISTS prompt;
