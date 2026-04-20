ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS request_hash TEXT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS ttft_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS proxy_overhead_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS duration_ms INT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_per_second FLOAT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS virtual_key_name TEXT;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS prompt TEXT;

CREATE INDEX IF NOT EXISTS idx_request_logs_request_hash ON request_logs(request_hash);
CREATE INDEX IF NOT EXISTS idx_request_logs_created_at_retention ON request_logs(created_at);
