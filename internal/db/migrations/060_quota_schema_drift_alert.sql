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
-- Both predicates compare against a whitespace-stripped copy of the value, never
-- against the value itself, so the stored CSV survives byte for byte apart from
-- the appended entry. Stripping *all* whitespace rather than only spaces keeps
-- this in step with alert.ParseEnabled, which trims every entry: anything
-- ParseEnabled reads as an entry has to be read as one here too, or a
-- tab-padded row gains a duplicate and a whitespace-only row gets appended to.
--
-- The states an install can be in, all covered by
-- TestQuotaSchemaDriftMigrationAppendsToASavedSelection:
--   * key absent       -> no row is created; that install already gets the event
--                         from DefaultEnabledCSV(), and materializing a row would
--                         freeze its selection at today's defaults.
--   * customized       -> the stored CSV is preserved verbatim, in order, with
--                         the new type appended.
--   * already present  -> untouched. The check wraps the stripped value in commas
--                         and matches a whole entry, so it is idempotent, is not
--                         fooled by a longer lookalike such as
--                         quota.schema_drift_v2, and recognises an entry padded
--                         with spaces, tabs or newlines.
--   * nothing selected -> untouched. An empty value is the operator deselecting
--                         everything, and appending would switch an alert back on
--                         against that choice. Trimming the separators extends
--                         that to every value ParseEnabled also reads as empty:
--                         '', '   ', ',', ' , ,<tab>'.
--
-- strpos, not LIKE: '_' is a single-character wildcard in LIKE, so a LIKE
-- pattern containing 'schema_drift' would also match 'schemaXdrift'.
--
-- The CASE on the separator keeps a value that already ends in a comma from
-- gaining a blank entry. ParseEnabled skips blanks, so that is tidiness rather
-- than behaviour, but this row is what the operator sees in the picker.
--
-- Keep this in step with the DefaultOn entries in internal/alert/catalog.go;
-- TestQuotaSchemaDriftMigrationMatchesTheCatalog guards the pairing.
UPDATE settings
SET value = value
         || CASE WHEN value ~ ',\s*$' THEN '' ELSE ',' END
         || 'quota.schema_drift',
    updated_at = now()
WHERE key = 'alert_events'
  AND btrim(regexp_replace(value, '\s', '', 'g'), ',') <> ''
  AND strpos(',' || regexp_replace(value, '\s', '', 'g') || ',', ',quota.schema_drift,') = 0;
