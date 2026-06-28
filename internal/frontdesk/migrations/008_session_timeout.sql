-- Front Desk admin-UI inactivity auto-logout. The control plane signs the
-- operator out after this many minutes with no activity in the browser; 0
-- disables auto-logout (sessions stay open). Consumed by the frontend
-- (useIdleLogout); the server only stores and bounds-checks the value.
--
-- session_idle_timeout_minutes : minutes of inactivity before auto-logout
--                                (0 = never). Default 60.
ALTER TABLE settings ADD COLUMN session_idle_timeout_minutes INTEGER NOT NULL DEFAULT 60;
