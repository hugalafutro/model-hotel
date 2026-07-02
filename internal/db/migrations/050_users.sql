-- Multi-user support: named dashboard users with roles and per-user feature
-- grants. The env admin token remains a break-glass superadmin outside this
-- table; rows here are additional identities managed from the dashboard.
-- password_hash holds a self-describing argon2id encoded string
-- ($argon2id$v=19$m=...,t=...,p=...$salt$hash), so parameters can be upgraded
-- later without a schema change.
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL DEFAULT '',
    email         TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    grants        TEXT[] NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

-- SSO email binding is looked up on every OIDC/GitHub login.
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email) WHERE email IS NOT NULL;
