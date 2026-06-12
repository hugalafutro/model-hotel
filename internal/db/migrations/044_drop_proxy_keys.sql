-- Drop the unused proxy_keys table.
-- Created in migration 001 for client API keys but never read or written by
-- any application code; the concept shipped as virtual_keys (migration 004).
DROP TABLE IF EXISTS proxy_keys;
