-- Per-user TOTP (RFC 6238) second factor for dashboard user accounts.
-- Mirrors admin_totp (048/049) but keyed by users.id: secret is AES-GCM
-- encrypted with MASTER_KEY, recovery codes stored as SHA-256 hashes only,
-- last_used_step enforces single use within the skew window. Rows cascade
-- away with the user. Instance-local by design: this table does NOT ride
-- fleet config-sync or backup, same stance as admin_totp.
CREATE TABLE IF NOT EXISTS user_totp (
    user_id        UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_cipher  BYTEA   NOT NULL,
    secret_nonce   BYTEA   NOT NULL,
    secret_salt    BYTEA   NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at   TIMESTAMPTZ,
    last_used_step BIGINT
);

-- Single-use recovery codes per user (shown once at enable time).
CREATE TABLE IF NOT EXISTS user_totp_recovery (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TIMESTAMPTZ,
    PRIMARY KEY (user_id, code_hash)
);
