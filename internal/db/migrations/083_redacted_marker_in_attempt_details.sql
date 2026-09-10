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
-- the detail field of each attempt is rewritten, not the whole JSON text: a
-- provider or model name is data this migration has no business editing, however
-- unlikely such a name is. Only the attempts column is touched, because no
-- error_message row carries the marker: the fence never ran over the terminal
-- message.
--
-- The column carries no array constraint, and a WHERE clause is not evaluation
-- order, so every jsonb_array_elements call is fed a value normalized to an
-- array first. One row holding an object or a scalar would otherwise abort the
-- migration and with it the startup that runs it.
UPDATE request_logs AS l
SET attempts = (
    SELECT jsonb_agg(
        CASE
            WHEN jsonb_typeof(a -> 'detail') = 'string' AND a ->> 'detail' LIKE '%[content]%'
                THEN jsonb_set(a, '{detail}', to_jsonb(REPLACE(a ->> 'detail', '[content]', '[redacted]')))
            ELSE a
        END
        ORDER BY ord
    )
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(l.attempts) = 'array' THEN l.attempts ELSE '[]'::jsonb END
    ) WITH ORDINALITY AS t(a, ord)
)
WHERE EXISTS (
    SELECT 1
    FROM jsonb_array_elements(
        CASE WHEN jsonb_typeof(l.attempts) = 'array' THEN l.attempts ELSE '[]'::jsonb END
    ) AS e
    WHERE jsonb_typeof(e -> 'detail') = 'string'
      AND e ->> 'detail' LIKE '%[content]%'
);
