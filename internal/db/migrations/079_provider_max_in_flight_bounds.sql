-- The per-provider in-flight ceiling is bounded at the column, mirroring the
-- rule every write path applies (provider.ValidateMaxInFlight: NULL for no
-- ceiling, else 1..10000). The runtime reads a ceiling of zero or less as "no
-- ceiling", so a value below one stored by any path (the config import wrote
-- the envelope's value verbatim) silently deleted the operator's cap and then
-- exported to every member on the next sync. Rows already out of range are
-- returned to "no ceiling" first, which is what the runtime treated them as.
UPDATE providers SET max_in_flight = NULL WHERE max_in_flight IS NOT NULL AND (max_in_flight < 1 OR max_in_flight > 10000);
ALTER TABLE providers ADD CONSTRAINT providers_max_in_flight_bounds CHECK (max_in_flight IS NULL OR (max_in_flight BETWEEN 1 AND 10000));
