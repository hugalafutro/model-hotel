-- Marks a model that the PROXY retired from live traffic: the provider answered
-- a real request by saying it no longer serves this model, three times running,
-- with no success in between (see internal/proxy/model_gone.go). NULL means "not
-- retired by traffic", which covers an enabled model, one discovery disabled for
-- vanishing from a listing, and one the operator switched off by hand.
--
-- The column exists because enabled + disabled_manually cannot express three
-- states, and the retirement needs all three kept apart:
--
--   * Operator disable (disabled_manually = true) must never be undone by
--     anything automatic, and must not nag the operator about their own choice.
--   * Discovery disable (both flags false) SHOULD be undone by a re-sighting:
--     the model vanished from the provider's listing, so its reappearance is
--     genuine new evidence.
--   * Traffic retirement is neither. Setting disabled_manually would make it
--     permanent AND invisible — the model stays off after the provider restores
--     it, with nothing surfaced for anyone to act on. Leaving both flags false
--     lets Upsert revive it on the next scan, and that is worse than it sounds:
--     the evidence that retired it was the provider REFUSING the model while
--     still listing it, so a re-sighting is not new evidence at all. The model
--     returns to routing, fails again, re-alerts and churns failover groups on
--     every scan.
--
-- So this is the same distinction migration 062 draws for failover groups, for
-- the same reason: the flag alone cannot answer "who did this".
--
-- Like models.discovery_dismissed_at and model_failover_groups.auto_disabled_at,
-- this is persisted INTENT, not a stored claim. Every operator-driven write of
-- models.enabled clears it back to NULL (Repository.SetEnabled and
-- Repository.Update), so a model that is retired, re-enabled by hand, and
-- retired again reads as a fresh retirement rather than inheriting a stale
-- stamp. Re-enabling by hand is therefore also how an operator tells the gateway
-- to trust the provider's listing again.
--
-- No backfill: before this migration no model was ever retired from traffic, so
-- there is no historical provenance to recover.
ALTER TABLE models ADD COLUMN IF NOT EXISTS auto_retired_at TIMESTAMPTZ;

-- Partial index: every read of this column asks the same question — is this
-- disabled model one the proxy retired?
CREATE INDEX IF NOT EXISTS idx_models_auto_retired
    ON models (provider_id)
    WHERE enabled = false AND auto_retired_at IS NOT NULL;
