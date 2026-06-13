-- Add a machine-readable error classification alongside the free-text
-- error_message. The dashboard's "Interrupted" badge and (later) Prometheus
-- metrics read this instead of substring-matching the English message.
--
-- Nullable on purpose: historical rows stay NULL and the frontend falls back to
-- substring matching for them; no backfill (see plans/logging-and-errors-overhaul.md §7).
ALTER TABLE request_logs ADD COLUMN error_kind TEXT;
