-- Consecutive-failure damping for member reachability. Front Desk must observe
-- this many failed health polls in a row before it reports a member down (an
-- error event plus, by default, an Apprise alert). This tolerates the brief
-- unreachability of a routine container rebuild without flapping; recovery is
-- immediate (the first healthy poll clears the count). The same threshold damps
-- the Traefik UP -> DOWN badge flip. Default 3 (~15s at the 5s poll interval).
--
-- health_fail_threshold : consecutive failed polls before a member is reported
--                         down. Bounded to at least 1 by the store.
ALTER TABLE settings ADD COLUMN health_fail_threshold INTEGER NOT NULL DEFAULT 3;
