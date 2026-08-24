-- attrs_at is the offset in app_logs.message where the encoded attribute
-- suffix begins, recorded by the slog handler that built the row, counted in
-- UTF-16 code units (the unit JavaScript strings index by, so the dashboard
-- slices on the same boundary for non-ASCII text). Everything
-- before it is raw developer-written message text; everything from it on is
-- quoteLogValue output. The dashboard decodes the \x20 space escaping ONLY
-- from this offset, so raw message text is never altered no matter what
-- characters it contains, and attributes always decode. Meaningful only on
-- rows with escaped = TRUE; legacy rows keep the default.
ALTER TABLE app_logs ADD COLUMN IF NOT EXISTS attrs_at INTEGER NOT NULL DEFAULT 0;
