-- Consecutive-scan miss counter for discovery. A model absent from one provider
-- listing is no longer disabled immediately: each confirmed-missing scan (after
-- in-scan confirmation probes) increments missing_scans, and the model is only
-- disabled when the streak reaches the threshold (model.MissingScanThreshold).
-- Any sighting (Upsert) resets the counter to 0. This stops one flaky DNS
-- lookup or partial upstream listing from disabling models fleet-wide.
ALTER TABLE models ADD COLUMN IF NOT EXISTS missing_scans INTEGER NOT NULL DEFAULT 0;
