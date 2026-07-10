-- Stable identity of the model-hotel instance behind each member, read from the
-- member's /api/system (instance_id). Front Desk uses it to recognise the same
-- physical instance reached under a different URL, so it can refuse to add a host
-- that is already a member (or the primary) even when the URL string differs.
-- Empty until first learned by a probe; backfilled for pre-existing members the
-- next time Front Desk verifies them.
ALTER TABLE members ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';
