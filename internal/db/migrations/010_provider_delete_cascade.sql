-- Allow deleting providers even when request_logs reference them
-- Set provider_id to NULL instead of blocking the delete
ALTER TABLE request_logs DROP CONSTRAINT IF EXISTS request_logs_provider_id_fkey;
ALTER TABLE request_logs ADD CONSTRAINT request_logs_provider_id_fkey
    FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE SET NULL;