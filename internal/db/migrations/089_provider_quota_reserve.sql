-- Quota reserve: the share of every subscription window the operator keeps
-- back for use outside the gateway. 0 (the default) drains a window fully
-- before the breaker pins the provider; 10..90 pins it once that much is left.
ALTER TABLE providers ADD COLUMN quota_reserve_percent smallint NOT NULL DEFAULT 0
    CHECK (quota_reserve_percent BETWEEN 0 AND 90 AND quota_reserve_percent % 10 = 0);
