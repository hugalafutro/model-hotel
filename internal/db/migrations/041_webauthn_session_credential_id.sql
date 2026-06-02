-- Add credential_id column to webauthn_sessions so that deleting a passkey
-- can cascade-revoke any auth-token sessions derived from that credential's
-- login ceremony (review feedback: without this, a compromised passkey's
-- sessions remain valid for their full 30-day TTL after deletion).
ALTER TABLE webauthn_sessions ADD COLUMN IF NOT EXISTS credential_id BYTEA;

CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_credential_id
    ON webauthn_sessions(credential_id)
    WHERE credential_id IS NOT NULL;
