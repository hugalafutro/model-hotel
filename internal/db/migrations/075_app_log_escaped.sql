-- escaped marks app_logs rows whose message was produced by the slog handler,
-- i.e. whose attribute values use the flattened quoteLogValue encoding
-- (spaces inside quoted values stored as \x20). The dashboard decodes that
-- escaping for display ONLY on rows carrying this flag, so a legacy or raw
-- line containing a literal \x20 is never altered: rows predating the column
-- (and lines ingested through the legacy io.Writer path) stay FALSE and
-- render verbatim.
ALTER TABLE app_logs ADD COLUMN IF NOT EXISTS escaped BOOLEAN NOT NULL DEFAULT FALSE;
