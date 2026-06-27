-- Remove sticky sessions. The setting pinned a browser dashboard (and its SSE
-- stream) to one backend, but the load balancer now serves only the
-- OpenAI-compatible proxy (router rule PathPrefix(`/v1`)): dashboards are reached
-- per-member by their own URL and never traverse the LB, and /v1 API clients do
-- not carry the mh_lb cookie. The toggle therefore did nothing useful, so the
-- column, its Traefik config, and the UI control are all removed.
ALTER TABLE settings DROP COLUMN sticky_enabled;
