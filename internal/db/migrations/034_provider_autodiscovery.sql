-- Add per-provider autodiscovery toggle.
-- When false, the provider is excluded from model discovery (both single and "discover all"),
-- but remains enabled for proxy routing.
ALTER TABLE providers ADD COLUMN autodiscovery_enabled BOOLEAN NOT NULL DEFAULT true;
