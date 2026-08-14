-- manually_enabled_at is the operator's pin on a model the provider's listing
-- no longer names: set when an operator enables a model by hand, cleared by an
-- operator disable, by an explicit unpin, and by any discovery sighting. While
-- set, RecordMissingModels keeps counting misses but never disables the row.
-- It does not shield against traffic retirement (auto_retired_at): a refusal
-- by name on a real request outranks the operator's earlier manual test.
ALTER TABLE models ADD COLUMN IF NOT EXISTS manually_enabled_at TIMESTAMPTZ;
