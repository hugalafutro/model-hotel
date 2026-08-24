-- request_logs.virtual_key_id was deliberately unindexed while it only served
-- the owner-scope disjunction (see migration 067's note). The Logs page now
-- offers a first-class virtual-key filter dropdown, so every filtered page
-- costs a predicate scan over the busiest table in the schema (data query
-- plus its count twin); index the column the same way 067 indexed the filter
-- it shipped. Partial for the same reason as idx_request_logs_owner: the
-- filter is an equality match, which a NULL row can never satisfy, so
-- indexing keyless rows would be pure overhead.
CREATE INDEX IF NOT EXISTS idx_request_logs_virtual_key_id
    ON request_logs (virtual_key_id) WHERE virtual_key_id IS NOT NULL;
