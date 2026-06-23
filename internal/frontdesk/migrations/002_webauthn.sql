-- SQLite mirror of the main server's webauthn_credentials / webauthn_sessions
-- schema (migrations 039-041). The column set and semantics match the Postgres
-- tables so the reused webauthn.SessionManager and passkey logic behave
-- identically; only the types are adapted to SQLite (BYTEA -> BLOB, UUID/TEXT[]
-- stored as TEXT, TIMESTAMPTZ -> INTEGER Unix nanoseconds).

CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id                           BLOB PRIMARY KEY,
    name                         TEXT NOT NULL DEFAULT '',
    public_key                   BLOB NOT NULL,
    attestation_type             TEXT NOT NULL DEFAULT '',
    attestation_format           TEXT NOT NULL DEFAULT '',
    -- transport is a JSON array of strings (Postgres uses TEXT[]).
    transport                    TEXT NOT NULL DEFAULT '[]',
    flags_byte                   INTEGER NOT NULL DEFAULT 0,
    sign_count                   INTEGER NOT NULL DEFAULT 0,
    aaguid                       TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    attestation_object           BLOB,
    attestation_client_data      BLOB,
    attestation_client_data_hash BLOB,
    attestation_public_key_algo  INTEGER,
    authenticator_data           BLOB,
    created_at                   INTEGER NOT NULL,
    updated_at                   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id            TEXT PRIMARY KEY,
    challenge     TEXT NOT NULL,
    session_data  BLOB NOT NULL,
    type          TEXT NOT NULL,
    user_id       BLOB,
    token_hash    TEXT,
    credential_id BLOB,
    expires_at    INTEGER NOT NULL,
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wa_sessions_expires ON webauthn_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_wa_sessions_type ON webauthn_sessions(type);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wa_sessions_token_hash
    ON webauthn_sessions(token_hash) WHERE token_hash IS NOT NULL;
