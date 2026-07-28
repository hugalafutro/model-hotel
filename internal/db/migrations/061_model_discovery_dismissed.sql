-- Operator dismissal for a discovery discrepancy ("yes, this model is gone for
-- good, stop counting it"). NULL means not dismissed.
--
-- Claims themselves are NOT stored: a pending claim is derived from
-- (enabled = false AND disabled_manually = false), so it can never drift from
-- what discovery actually believes. This column is the one piece of operator
-- intent that cannot be derived, and models.Upsert clears it on any sighting so
-- a dismissed model that reappears and vanishes again reads as a fresh claim
-- rather than staying silently suppressed.
ALTER TABLE models ADD COLUMN IF NOT EXISTS discovery_dismissed_at TIMESTAMPTZ;

-- Partial index: the claim query only ever looks at discovery-disabled rows.
CREATE INDEX IF NOT EXISTS idx_models_discovery_claims
    ON models (provider_id) WHERE NOT enabled AND NOT disabled_manually;
