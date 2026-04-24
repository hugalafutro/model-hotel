-- Clean up any request logs stuck in "pending" or "streaming" state.
-- These are requests that were interrupted by a server crash, restart, or
-- unhandled error before their final state could be written.
-- We use a generous 30-minute window to avoid marking legitimate long-running
-- streaming requests as failed (some providers can take minutes to first token).
UPDATE request_logs
SET state         = 'failed',
    error_message = 'request interrupted (server restart)'
WHERE state IN ('pending', 'streaming')
  AND created_at < NOW() - INTERVAL '30 minutes';
