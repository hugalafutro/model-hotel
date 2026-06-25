-- Front Desk control-plane core schema: the member list, the control-plane
-- event log, and the single-row settings that shape the generated Traefik
-- config. Timestamps are stored as INTEGER Unix nanoseconds so comparisons and
-- ordering are exact and locale-independent.

CREATE TABLE IF NOT EXISTS members (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    url          TEXT NOT NULL UNIQUE,
    state        TEXT NOT NULL DEFAULT 'active',
    -- AES-256-GCM encrypted member admin token (optional, per member). All three
    -- columns are NULL together when no token is stored.
    token_cipher BLOB,
    token_nonce  BLOB,
    token_salt   BLOB,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    severity   TEXT NOT NULL,
    source     TEXT NOT NULL,
    message    TEXT NOT NULL,
    metadata   TEXT,
    member_id  TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
CREATE INDEX IF NOT EXISTS idx_events_member_id ON events(member_id);

-- Single-row settings table (id is pinned to 1). Seeded with defaults on first
-- migration; every field has a sensible default so a fresh install is usable
-- without touching this tab.
CREATE TABLE IF NOT EXISTS settings (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    health_poll_secs     INTEGER NOT NULL DEFAULT 5,
    traefik_poll_secs    INTEGER NOT NULL DEFAULT 5,
    traefik_stale_secs   INTEGER NOT NULL DEFAULT 30,
    event_retention_days INTEGER NOT NULL DEFAULT 90,
    retry_attempts       INTEGER NOT NULL DEFAULT 2,
    sticky_enabled       INTEGER NOT NULL DEFAULT 1
);
INSERT INTO settings (id) VALUES (1) ON CONFLICT(id) DO NOTHING;
