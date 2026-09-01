-- Per-provider hard ceiling on concurrent in-flight requests, for the operator
-- who knows the provider's real slot count. NULL means no ceiling: the
-- adaptive in-flight limiter still learns underneath either way, and the
-- ceiling only bounds what it may admit.
ALTER TABLE providers ADD COLUMN max_in_flight integer;
