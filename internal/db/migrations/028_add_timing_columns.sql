-- Add timing columns for SafeDialer DNS resolution and settings reads.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS safe_dial_ms DOUBLE PRECISION DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS settings_read_ms DOUBLE PRECISION DEFAULT 0;