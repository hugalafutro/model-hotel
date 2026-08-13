-- Device metadata on admin auth-token sessions, so the dashboard can list
-- active sessions ("Firefox on Linux, last seen 5 minutes ago") instead of
-- offering only a blind revoke-others button.
--
-- user_agent   : User-Agent header captured when the session was minted
-- ip           : client IP captured when the session was minted (display
--                metadata for the operator, never an authorization input)
-- last_seen_at : stamped on successful token validation, throttled server-side
--                to one write per session per few minutes; NULL until the
--                session authenticates a request after this migration
--
-- Existing rows keep empty strings / NULL and render as an unknown device.
ALTER TABLE webauthn_sessions ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE webauthn_sessions ADD COLUMN IF NOT EXISTS ip TEXT NOT NULL DEFAULT '';
ALTER TABLE webauthn_sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
