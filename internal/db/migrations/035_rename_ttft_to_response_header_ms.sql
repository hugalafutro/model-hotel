-- Rename old ttft_ms (which actually measures time-to-HTTP-headers) to response_header_ms.
-- Add new ttft_ms column for true time-to-first-token measurement.
ALTER TABLE request_logs RENAME COLUMN ttft_ms TO response_header_ms;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS ttft_ms REAL DEFAULT 0;
