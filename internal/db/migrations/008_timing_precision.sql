-- Convert timing columns from INT to REAL for sub-ms precision
-- These columns were originally INT (migration 001 and 006) or added as INT by migration 007
ALTER TABLE request_logs ALTER COLUMN latency_ms TYPE REAL USING latency_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN duration_ms TYPE REAL USING duration_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN ttft_ms TYPE REAL USING ttft_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN proxy_overhead_ms TYPE REAL USING proxy_overhead_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN parse_ms TYPE REAL USING parse_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN model_lookup_ms TYPE REAL USING model_lookup_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN provider_lookup_ms TYPE REAL USING provider_lookup_ms::REAL;
ALTER TABLE request_logs ALTER COLUMN key_decrypt_ms TYPE REAL USING key_decrypt_ms::REAL;