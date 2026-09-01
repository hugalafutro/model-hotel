-- Per-attempt trail on the request log: one JSON element per failover attempt
-- (hedged probes, in-flight busy skips and the saturation retry included), so
-- a 429 that was attempt 0 of a request another provider then served is no
-- longer invisible. Written once at terminal time; the flat columns keep the
-- terminal attempt's values, so nothing that reads them changes.
--
-- Nullable on purpose: rows from before this migration, and rows written by an
-- older binary during a mixed-version rollout, stay NULL and the dashboard
-- shows no trail for them. No backfill.
ALTER TABLE request_logs ADD COLUMN attempts JSONB;

-- jsonb_path_ops serves the containment filters the logs API adds
-- (attempt_provider_id / attempt_status): attempts @> '[{"provider_id": ...}]'.
CREATE INDEX IF NOT EXISTS idx_request_logs_attempts ON request_logs USING GIN (attempts jsonb_path_ops);
