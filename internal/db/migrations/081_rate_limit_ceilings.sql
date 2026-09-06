-- Clamp per-key and per-user rate limits that sit above the ceilings the
-- interactive API now enforces (validateRateLimits in internal/api/virtualkeys.go
-- reads them from allowedSettings: rps 10000, burst 10000, tpm 100000000, the
-- same maxima the global settings have carried since they got bounds).
--
-- Unlike the floors migration 064 cleaned up, this window was open on the
-- interactive path itself: every writer validated minimums only, so a key or
-- account whose limit was set to some huge number as an "unlimited" stand-in is
-- an ordinary row, not a hazard. It becomes one now: the edit modals resubmit
-- every field, so an above-ceiling row would answer 400 to a rename or a
-- disable until the operator lowers the limit by hand. Clamping is
-- meaning-preserving here in the direction 064 could not use: the ceiling is
-- already effectively unlimited, so LEAST() keeps the row an "unlimited" row
-- while making it editable again.
--
-- No CHECK constraint on purpose. The config-sync import deliberately accepts
-- above-ceiling values (validateSyncedRateLimits, floors only) so that a newer
-- primary raising a ceiling never makes an older member reject the entire
-- envelope; a schema ceiling would reject that same envelope at the DB. The
-- ceiling is an interactive-API guard, and the API is where it lives.
UPDATE virtual_keys SET rate_limit_rps   = LEAST(rate_limit_rps,   10000)     WHERE rate_limit_rps   > 10000;
UPDATE virtual_keys SET rate_limit_burst = LEAST(rate_limit_burst, 10000)     WHERE rate_limit_burst > 10000;
UPDATE virtual_keys SET rate_limit_tpm   = LEAST(rate_limit_tpm,   100000000) WHERE rate_limit_tpm   > 100000000;

UPDATE users SET rate_limit_rps   = LEAST(rate_limit_rps,   10000)     WHERE rate_limit_rps   > 10000;
UPDATE users SET rate_limit_burst = LEAST(rate_limit_burst, 10000)     WHERE rate_limit_burst > 10000;
UPDATE users SET rate_limit_tpm   = LEAST(rate_limit_tpm,   100000000) WHERE rate_limit_tpm   > 100000000;
