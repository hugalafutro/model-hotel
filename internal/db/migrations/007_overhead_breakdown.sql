ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS parse_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model_lookup_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_lookup_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS key_decrypt_ms INT;