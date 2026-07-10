-- Stable per-instance identity. Front Desk stamps this onto each member it
-- tracks and uses it to recognise the same physical instance reached under a
-- different URL (public DNS vs a LAN address): it refuses to add a host that is
-- already the fleet primary or already a member, independently of the URL string.
-- Generated once here and never changed; exposed (admin-authenticated) on
-- /api/system as instance_id. gen_random_uuid() is core in PostgreSQL 13+.
INSERT INTO settings (key, value)
VALUES ('instance_id', gen_random_uuid()::text)
ON CONFLICT (key) DO NOTHING;
