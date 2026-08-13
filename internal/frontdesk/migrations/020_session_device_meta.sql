-- Device metadata on admin auth-token sessions, mirroring the main server's
-- migration 069 so the shared session list/revoke logic behaves identically on
-- Front Desk's SQLite store.
--
-- user_agent   : User-Agent header captured when the session was minted
-- ip           : client IP captured when the session was minted (display
--                metadata for the operator, never an authorization input)
-- last_seen_at : UnixNano UTC, stamped on successful token validation
--                (throttled); NULL until the session authenticates a request
--                after this migration
ALTER TABLE webauthn_sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE webauthn_sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE webauthn_sessions ADD COLUMN last_seen_at INTEGER;
