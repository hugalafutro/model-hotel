ALTER TABLE models ADD COLUMN IF NOT EXISTS disabled_manually BOOLEAN DEFAULT false;

-- Preserve existing state: mark currently-disabled models as manually disabled
-- so discovery doesn't re-enable them after migration
UPDATE models SET disabled_manually = true WHERE enabled = false;
