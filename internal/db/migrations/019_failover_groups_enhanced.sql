-- Failover Groups Enhancement
-- Adds group-level enable toggle, per-entry enabled map, and metadata fields

-- Add group-level enable toggle
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS group_enabled BOOLEAN DEFAULT true;

-- Add per-entry enabled map: {model_uuid: true/false}
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS entry_enabled JSONB DEFAULT '{}';

-- Add display_name (human-readable, can differ from display_model)
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS display_name TEXT;

-- Add auto_created flag
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS auto_created BOOLEAN DEFAULT false;

-- Add description
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';

-- Initialize existing rows with defaults
UPDATE model_failover_groups 
SET 
    group_enabled = COALESCE(group_enabled, true),
    entry_enabled = COALESCE(entry_enabled, '{}'),
    auto_created = COALESCE(auto_created, false),
    description = COALESCE(description, '')
WHERE group_enabled IS NULL OR entry_enabled IS NULL OR auto_created IS NULL OR description IS NULL;