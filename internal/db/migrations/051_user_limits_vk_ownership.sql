-- Phase 2 of multi-user: virtual-key ownership + per-user token/rate limits.
--
-- A virtual key may belong to a dashboard user. The owner's limits then apply
-- in aggregate across every key they own, on top of the per-key limits.
-- Deleting a user orphans their keys (owner NULL) rather than deleting them,
-- so an account cleanup cannot silently kill production traffic.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

-- Owner lookup happens on the proxy hot path (join in FindByKeyHash) and in
-- the ownership-filtered key listing.
CREATE INDEX IF NOT EXISTS idx_virtual_keys_owner
    ON virtual_keys (owner_user_id) WHERE owner_user_id IS NOT NULL;

-- Per-user aggregate limits, mirroring the per-key fields. NULL = no cap;
-- unlike the per-key fields there is no global-settings fallback for these.
ALTER TABLE users ADD COLUMN IF NOT EXISTS rate_limit_rps DOUBLE PRECISION;
ALTER TABLE users ADD COLUMN IF NOT EXISTS rate_limit_burst INTEGER;
ALTER TABLE users ADD COLUMN IF NOT EXISTS rate_limit_tpm INTEGER;
