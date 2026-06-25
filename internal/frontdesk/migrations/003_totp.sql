-- SQLite mirror of the main server's admin_totp / admin_totp_recovery schema
-- (migrations 048-049). Same single-row (id = 1) shape and recovery-code table,
-- so the reused totp.Repository crypto and single-use enforcement behave
-- identically. TIMESTAMPTZ columns become INTEGER Unix nanoseconds.

CREATE TABLE IF NOT EXISTS admin_totp (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    secret_cipher  BLOB NOT NULL,
    secret_nonce   BLOB NOT NULL,
    secret_salt    BLOB NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    confirmed_at   INTEGER,
    last_used_step INTEGER
);

CREATE TABLE IF NOT EXISTS admin_totp_recovery (
    code_hash TEXT PRIMARY KEY,
    used_at   INTEGER
);
