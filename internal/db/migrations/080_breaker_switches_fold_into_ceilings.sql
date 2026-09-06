-- The circuit breaker's quota-pin and probe-backoff switches fold into their
-- ceilings: a circuit_breaker_quota_pin_max or circuit_breaker_backoff_max of
-- zero is now the off switch, and an absent ceiling still means the built-in
-- default (24h and 15m). The runtime used to read a non-positive ceiling as
-- unset, so a stored zero never meant anything before this migration and the
-- rewrite loses nothing.
--
-- A member whose switch was off keeps that: its ceiling becomes zero whether
-- or not a ceiling row existed. Then the retired switch rows go, so they stop
-- shipping in config-sync envelopes and backups.
INSERT INTO settings (key, value)
SELECT c.ceiling, '0s'
FROM (VALUES
    ('circuit_breaker_quota_pin_enabled', 'circuit_breaker_quota_pin_max'),
    ('circuit_breaker_backoff_enabled', 'circuit_breaker_backoff_max')
) AS c(switch, ceiling)
JOIN settings s ON s.key = c.switch AND s.value = 'false'
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

DELETE FROM settings
WHERE key IN ('circuit_breaker_quota_pin_enabled', 'circuit_breaker_backoff_enabled');
