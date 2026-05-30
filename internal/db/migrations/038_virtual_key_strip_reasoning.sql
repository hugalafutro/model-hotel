-- Add per-key strip_reasoning flag to virtual_keys.
-- When true, reasoning/reasoning_content fields are stripped from streaming output for this key.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS strip_reasoning BOOLEAN NOT NULL DEFAULT false;
