CREATE TABLE IF NOT EXISTS virtual_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    key_hash      TEXT NOT NULL UNIQUE,
    key_preview   TEXT NOT NULL,
    tokens_used   BIGINT NOT NULL DEFAULT 0,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_virtual_keys_key_hash ON virtual_keys(key_hash);
