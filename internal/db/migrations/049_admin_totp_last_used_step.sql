-- Single-use enforcement for admin TOTP (RFC 6238 §5.2): record the most
-- recently accepted 30-second step so a code cannot be replayed within the
-- skew window. NULL means no code has been accepted yet.
ALTER TABLE admin_totp ADD COLUMN IF NOT EXISTS last_used_step BIGINT;
