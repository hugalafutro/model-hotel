-- Add UNIQUE constraint on provider name to prevent duplicates
-- Provider names are used in routing (e.g., model IDs like "provider-name/model-id")
-- and must be unique to avoid ambiguous resolution.

ALTER TABLE providers ADD CONSTRAINT providers_name_unique UNIQUE (name);
