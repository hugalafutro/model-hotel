-- WebAuthn/FIDO2 credential storage and session management
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id BYTEA PRIMARY KEY,
    public_key BYTEA NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT '',
    attestation_format TEXT NOT NULL DEFAULT '',
    transport TEXT[] NOT NULL DEFAULT '{}',
    flags_byte SMALLINT NOT NULL DEFAULT 0,
    sign_count INTEGER NOT NULL DEFAULT 0,
    aaguid UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    attestation_object BYTEA,
    attestation_client_data BYTEA,
    attestation_client_data_hash BYTEA,
    attestation_public_key_algo BIGINT,
    authenticator_data BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    challenge TEXT NOT NULL,
    session_data BYTEA NOT NULL,
    type TEXT NOT NULL,
    user_id BYTEA,
    token_hash TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_expires ON webauthn_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_type ON webauthn_sessions(type);
CREATE UNIQUE INDEX IF NOT EXISTS idx_webauthn_sessions_token_hash ON webauthn_sessions(token_hash) WHERE token_hash IS NOT NULL;
