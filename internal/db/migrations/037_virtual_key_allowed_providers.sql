-- Add per-key provider access restriction to virtual_keys.
-- Nullable TEXT[]: when NULL, all providers are accessible. When set, only listed provider IDs are allowed.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS allowed_providers TEXT[] DEFAULT NULL;
