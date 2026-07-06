-- Consecutive mass-vanish counter for discovery's bulk-removal escalation. When
-- a provider's confirmed-missing set trips the mass-vanish guard (more than the
-- floor AND over half its enabled models absent even after confirmation probes),
-- the scan records no misses so a transiently broken listing cannot false-disable
-- models fleet-wide. But a provider that has genuinely sunset that many models
-- trips the guard every scan and would otherwise stay stale forever. suspect_scans
-- counts consecutive mass-vanish scans; once it reaches the escalation threshold a
-- distinct high-severity event is emitted so an operator can disable the sunset
-- models by hand. Any healthy scan (listing recovered) resets it to 0.
ALTER TABLE providers ADD COLUMN IF NOT EXISTS suspect_scans INTEGER NOT NULL DEFAULT 0;
