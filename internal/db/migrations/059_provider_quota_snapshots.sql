-- 059_provider_quota_snapshots.sql
-- Server-side cache of provider quota/usage so the dashboard shows fresh
-- numbers on load (including after a rebuild) without a live upstream call
-- in the request path. Holds only quota/usage metadata, never request content.
CREATE TABLE IF NOT EXISTS provider_quota_snapshots (
    provider_id     UUID        NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,               -- 'usage' | 'balance' | 'account'
    payload         JSONB,                              -- exact JSON body the endpoint returns
    http_status     SMALLINT    NOT NULL DEFAULT 200,   -- reproduce 200/204/424 on read-through
    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source          TEXT        NOT NULL DEFAULT 'poll',-- 'poll' | 'manual' | 'fleet'
    last_error      TEXT,
    last_attempt_at TIMESTAMPTZ,
    PRIMARY KEY (provider_id, kind)
);
