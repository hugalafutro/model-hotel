-- Allow keyless providers (e.g. OpenCode Zen free models) by making
-- encryption columns nullable. A provider without an API key stores
-- NULL in all three columns instead of placeholder bytes.

ALTER TABLE providers ALTER COLUMN encrypted_key DROP NOT NULL;
ALTER TABLE providers ALTER COLUMN key_nonce DROP NOT NULL;
