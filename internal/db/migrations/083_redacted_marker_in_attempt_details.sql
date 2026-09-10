-- Attempt trail details written before the content fence moved to the capture
-- points carry the marker "[content]" where the fence replaced a run of text an
-- upstream message shared with the request. The marker means the run was thrown
-- away, but it reads like the opposite: a log claiming to hold request content,
-- in a gateway whose headline promise is that it never stores any.
--
-- The rows are worse than merely ambiguous. What the old fence matched was
-- usually the gateway's OWN sentence, so a skipped candidate reads
-- "[content]open", the remains of "circuit breaker open" after the request
-- happened to contain a matching run. Nothing of the prompt is in there.
--
-- Rewrite the marker to "[redacted]", which says what actually happened. Only
-- the attempts column is touched: no error_message row carries the marker,
-- because the fence never ran over the terminal message.
UPDATE request_logs
SET attempts = REPLACE(attempts::text, '[content]', '[redacted]')::jsonb
WHERE attempts::text LIKE '%[content]%';
