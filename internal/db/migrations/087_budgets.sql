-- Dollar budgets on virtual keys and users: a spending cap per calendar period
-- (UTC day, week or month) over request_logs.cost_usd, enforced at admission
-- with a 429 once the period's spend reaches the cap. NULL = no budget. The
-- two columns travel together: a budget without a period has no meaning, and
-- a period without a budget is a leftover, so the CHECK keeps them paired.
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS budget_usd DOUBLE PRECISION;
ALTER TABLE virtual_keys ADD COLUMN IF NOT EXISTS budget_period TEXT;
ALTER TABLE virtual_keys DROP CONSTRAINT IF EXISTS virtual_keys_budget_check;
ALTER TABLE virtual_keys ADD CONSTRAINT virtual_keys_budget_check CHECK (
    (budget_usd IS NULL) = (budget_period IS NULL)
    AND (budget_usd IS NULL OR budget_usd > 0)
    AND (budget_period IS NULL OR budget_period IN ('day', 'week', 'month'))
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS budget_usd DOUBLE PRECISION;
ALTER TABLE users ADD COLUMN IF NOT EXISTS budget_period TEXT;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_budget_check;
ALTER TABLE users ADD CONSTRAINT users_budget_check CHECK (
    (budget_usd IS NULL) = (budget_period IS NULL)
    AND (budget_usd IS NULL OR budget_usd > 0)
    AND (budget_period IS NULL OR budget_period IN ('day', 'week', 'month'))
);
