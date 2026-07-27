-- Append the new default-on alert event quota.schema_drift to an operator's
-- saved alert selection.
--
-- Marking the catalog entry DefaultOn only reaches installs that have never
-- saved a picker selection: alert.AlertConfig seeds from DefaultEnabledCSV()
-- when alert_events is unset, and reads the stored CSV verbatim otherwise. So
-- without this, every install whose operator has ever touched the picker would
-- silently miss an event whose entire purpose is to report a silent failure --
-- and that is exactly the population running MiniMax or Ollama Cloud.
--
-- Front Desk migrations 015/016/017 set the precedent for adding a default-on
-- event to a stored CSV. They rewrite only when the value still equals the exact
-- previous seed, i.e. only when the operator never customized it. That shape
-- does not fit here: the main app has no seed migration to compare against, and
-- the ruling for this event is that a customized selection should receive it
-- too. So this appends instead, and never rewrites or reorders what is there.
--
-- Four states, all covered by TestQuotaSchemaDriftMigrationAppendsToASavedSelection:
--   * key absent      -> no row is created; that install already gets the event
--                        from DefaultEnabledCSV(), and materializing a row would
--                        freeze its selection at today's defaults.
--   * customized      -> the stored CSV is preserved verbatim, in order, with
--                        ',quota.schema_drift' appended.
--   * already present -> untouched. The check wraps the value in commas and
--                        matches a whole entry, so it is idempotent, is not
--                        fooled by a longer lookalike such as
--                        quota.schema_drift_v2, and tolerates a hand-edited CSV
--                        with spaces (ParseEnabled trims entries, so a spaced
--                        value must not gain a duplicate). Only the comparison
--                        strips spaces; the stored value is never rewritten.
--   * explicitly ''   -> untouched. An empty value is the operator deselecting
--                        everything, and appending would switch an alert back on
--                        against that choice.
--
-- strpos, not LIKE: '_' is a single-character wildcard in LIKE, so a LIKE
-- pattern containing 'schema_drift' would also match 'schemaXdrift'.
--
-- Keep this in step with the DefaultOn entries in internal/alert/catalog.go;
-- TestQuotaSchemaDriftMigrationMatchesTheCatalog guards the pairing.
UPDATE settings
SET value = value || ',quota.schema_drift',
    updated_at = now()
WHERE key = 'alert_events'
  AND value <> ''
  AND strpos(',' || replace(value, ' ', '') || ',', ',quota.schema_drift,') = 0;
