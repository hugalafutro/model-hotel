-- When the enabled auto-sync last found its primary unusable, as UnixNano UTC,
-- 0 while it can run or is off. Kept on disk so a Front Desk restarted during an
-- idle spell dates the spell from when it began rather than from the restart.
ALTER TABLE settings ADD COLUMN auto_sync_idle_since INTEGER NOT NULL DEFAULT 0;
