-- Add a reference to the virtual key used for each request-log
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS virtual_key_id UUID;
