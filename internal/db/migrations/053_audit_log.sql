-- Audit trail of admin actions: one row per mutating request (POST/PUT/
-- PATCH/DELETE) on the authenticated dashboard API, recorded by middleware.
-- Request bodies are NEVER stored (they carry provider keys, passwords, TOTP
-- codes); only who did what, where, and how the server answered.
-- Instance-local operational telemetry: not fleet-synced, not in backups,
-- pruned by the audit_retention_days setting (default 90).
CREATE TABLE IF NOT EXISTS audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor       TEXT NOT NULL,
    actor_role  TEXT NOT NULL,
    method      TEXT NOT NULL,
    route       TEXT NOT NULL,
    path        TEXT NOT NULL,
    entity_id   TEXT,
    status_code INT  NOT NULL,
    remote_addr TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log (created_at DESC, id DESC);
