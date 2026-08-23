-- client_ip is the proxied request's client address, resolved trusted-proxy
-- aware at ingest (internal/clientip: forwarded headers are honored only when
-- the TCP peer is a configured trusted proxy). Stored so key usage stays
-- attributable for as long as the request-log retention window, instead of
-- only in the fast-rotating app logs. NULL on rows predating this column and
-- on rows whose ingest path had no client address.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS client_ip TEXT;
