-- Dollar budgets on virtual keys and users: a spending cap per calendar period
-- (UTC day, week or month) over request_logs.cost_usd, enforced at admission
-- with a 429 once the period's spend reaches the cap. NULL = no budget. The
-- two columns travel together: a budget without a period has no meaning, and
-- a period without a budget is a leftover, so the CHECK keeps them paired and
-- holds the amount to the range the API accepts (budget.MaxUSD).
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS budget_usd DOUBLE PRECISION;
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS budget_period TEXT;
ALTER TABLE virtual_keys DROP CONSTRAINT IF EXISTS virtual_keys_budget_check;
ALTER TABLE virtual_keys ADD CONSTRAINT virtual_keys_budget_check CHECK (
    (budget_usd IS NULL) = (budget_period IS NULL)
    AND (budget_usd IS NULL OR (budget_usd > 0 AND budget_usd <= 10000000))
    AND (budget_period IS NULL OR budget_period IN ('day', 'week', 'month'))
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS budget_usd DOUBLE PRECISION;
ALTER TABLE users ADD COLUMN IF NOT EXISTS budget_period TEXT;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_budget_check;
ALTER TABLE users ADD CONSTRAINT users_budget_check CHECK (
    (budget_usd IS NULL) = (budget_period IS NULL)
    AND (budget_usd IS NULL OR (budget_usd > 0 AND budget_usd <= 10000000))
    AND (budget_period IS NULL OR budget_period IN ('day', 'week', 'month'))
);

-- A user's spend is summed by request_logs.owner_user_id alone, so that
-- reassigning or deleting a key leaves what was spent with whoever spent it.
-- Keyed rows carried no owner until now (the log views resolve them through
-- the key's current owner, and still do); stamp the current owner onto the
-- history once so the first period after the upgrade is not undercounted.
UPDATE request_logs rl
SET owner_user_id = vk.owner_user_id
FROM virtual_keys vk
WHERE rl.virtual_key_id = vk.id
  AND rl.owner_user_id IS NULL
  AND vk.owner_user_id IS NOT NULL;
