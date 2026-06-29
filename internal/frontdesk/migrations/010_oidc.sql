-- Front Desk OIDC SSO: a fourth admin-login path (alongside the FRONTDESK_TOKEN,
-- TOTP, and passkeys) through an external OpenID Connect provider. Reuses the
-- shared internal/adminauth OIDC handler and the same session seam the passkey /
-- TOTP logins mint into; this row just holds the per-instance config. GitHub SSO
-- is intentionally NOT brought to Front Desk (main-dashboard-only by design).
--
-- oidc_enabled          : master on/off for the SSO login button.
-- oidc_issuer_url       : the provider's issuer (discovery base).
-- oidc_client_id        : the OAuth client ID registered with the provider.
-- oidc_client_secret    : the client secret, encrypted at rest (auth.EncryptString,
--                         enc:v1: token) like the Apprise target; masked over the API.
-- oidc_public_base_url  : Front Desk's own public base URL; the redirect URI is
--                         <base>/api/auth/oidc/callback (registered with the provider).
-- oidc_allowed_emails   : CSV allowlist of verified emails permitted to sign in
--                         (fail-closed: empty denies everyone).
ALTER TABLE settings ADD COLUMN oidc_enabled         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN oidc_issuer_url      TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN oidc_client_id       TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN oidc_client_secret   TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN oidc_public_base_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN oidc_allowed_emails  TEXT    NOT NULL DEFAULT '';
