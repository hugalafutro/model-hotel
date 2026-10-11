-- Append the new default-on alert events quota.status_lost and
-- quota.status_restored to an operator's saved alert selection.
--
-- Same reasoning and same shape as 060_quota_schema_drift_alert.sql: marking a
-- catalog entry DefaultOn only reaches installs that never saved a picker
-- selection, and a lapsed subscription is exactly the silent failure an
-- operator with a customized selection still wants to hear about. The two
-- statements are independent and idempotent; each appends one whole entry only
-- when the stored CSV holds at least one real entry and does not already carry
-- that type (whole-entry match on a whitespace-stripped copy, strpos not LIKE
-- because '_' is a LIKE wildcard). An absent key gets no row, an empty or
-- separators-only value stays as the operator's "nothing" choice, and a value
-- already ending in a comma gains no blank entry.
--
-- Keep this in step with the DefaultOn entries in internal/alert/catalog.go;
-- TestQuotaStatusMigrationMatchesTheCatalog guards the pairing.
UPDATE settings
SET value = value
         || CASE WHEN value ~ ',\s*$' THEN '' ELSE ',' END
         || 'quota.status_lost',
    updated_at = now()
WHERE key = 'alert_events'
  AND btrim(regexp_replace(value, '\s', '', 'g'), ',') <> ''
  AND strpos(',' || regexp_replace(value, '\s', '', 'g') || ',', ',quota.status_lost,') = 0;

UPDATE settings
SET value = value
         || CASE WHEN value ~ ',\s*$' THEN '' ELSE ',' END
         || 'quota.status_restored',
    updated_at = now()
WHERE key = 'alert_events'
  AND btrim(regexp_replace(value, '\s', '', 'g'), ',') <> ''
  AND strpos(',' || regexp_replace(value, '\s', '', 'g') || ',', ',quota.status_restored,') = 0;
