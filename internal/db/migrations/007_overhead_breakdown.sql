ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS parse_ms REAL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model_lookup_ms REAL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_lookup_ms REAL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS key_decrypt_ms REAL;
ALTER TABLE request_logs ALTER COLUMN proxy_overhead_ms TYPE REAL USING proxy_overhead_ms::REAL;