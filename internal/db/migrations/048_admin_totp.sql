-- TOTP (RFC 6238) second-factor configuration for the single admin login.
-- Secret is AES-GCM encrypted with MASTER_KEY (see internal/auth/encryption.go),
-- mirroring how provider API keys are stored. The plaintext secret never hits disk.
CREATE TABLE IF NOT EXISTS admin_totp (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    secret_cipher BYTEA   NOT NULL,
    secret_nonce  BYTEA   NOT NULL,
    secret_salt   BYTEA   NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at  TIMESTAMPTZ
);

-- Single-use recovery codes (shown once at enable time). Store SHA-256 hashes only.
CREATE TABLE IF NOT EXISTS admin_totp_recovery (
    code_hash  TEXT PRIMARY KEY,
    used_at    TIMESTAMPTZ
);
