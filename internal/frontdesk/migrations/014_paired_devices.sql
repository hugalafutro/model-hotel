-- Paired devices (Bellhop phone pairing, Phase F0). Each row is one paired
-- device holding a device-scoped bearer token. Only the SHA-256 hash of the
-- token is stored (same treatment as the admin token and virtual keys); the
-- plaintext is returned exactly once by POST /api/pair. role is the
-- server-enforced ceiling ("monitor" read-only, "operator" adds the whitelisted
-- mutations). revoked_at is a soft delete: a revoked row stops authenticating
-- immediately but stays as an audit trail of what was paired.
CREATE TABLE IF NOT EXISTS paired_devices (
	id           TEXT PRIMARY KEY,
	label        TEXT NOT NULL,
	token_hash   TEXT NOT NULL UNIQUE,
	role         TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER,
	revoked_at   INTEGER
);
