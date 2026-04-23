ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS state TEXT DEFAULT 'pending';

-- Backfill existing rows
UPDATE request_logs SET state = 'completed'
WHERE state = 'pending' AND status_code IS NOT NULL AND status_code >= 200 AND status_code < 400 AND duration_ms > 0;

UPDATE request_logs SET state = 'failed'
WHERE state = 'pending' AND status_code IS NOT NULL AND status_code >= 400;

CREATE INDEX IF NOT EXISTS idx_request_logs_state ON request_logs(state);
