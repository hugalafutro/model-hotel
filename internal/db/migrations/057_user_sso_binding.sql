-- SSO provider identity binding. Previously an OIDC/GitHub login resolved a user
-- account by verified email alone (idx_users_email), so ANY configured provider
-- that could assert a victim's verified email would authenticate as that account
-- -- cross-provider identity confusion (a low-trust second IdP impersonating an
-- account provisioned via a trusted one). Record the (provider, subject) that
-- first logs in for an account and reject later logins whose identity differs,
-- even when the email matches. Columns stay NULL until an account's first SSO
-- login (trust-on-first-use); password-only accounts never set them.
ALTER TABLE users ADD COLUMN IF NOT EXISTS sso_provider TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS sso_subject  TEXT;

-- One account per external identity: a given (provider, subject) binds to at
-- most one user row, so a stolen subject cannot be pointed at a second account.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_sso_identity
    ON users (sso_provider, sso_subject)
    WHERE sso_provider IS NOT NULL;
