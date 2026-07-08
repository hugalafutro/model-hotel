# 👥 Multi-User Access

Model Hotel ships with a single shared admin token by default. Multi-user access adds named dashboard accounts on top of that: each user signs in with a username and password (plus their own optional TOTP second factor) on the same login screen, and is scoped to exactly the parts of the dashboard they need.

<p align="center">
<img src="screenshots/users.png" alt="Users page" width="760"><br>
<em>The Users page: one admin and several scoped user accounts, with role, grants, status, and last-login columns</em>
</p>

## Overview

- Named accounts live in the `users` table, separate from the env admin token.
- Two roles: **admin** (full access, every grant implied) and **user** (access bounded by a grant list).
- A user **owns** their virtual keys. Per-account rate limits (RPS / burst / TPM) cap the aggregate traffic across the keys that user owns.
- The username/password login form appears on the login screen only once at least one user exists. A fresh install keeps the single admin-token flow.
- Local admin-token login is never removed, so a locked-out or misconfigured user can never lock you out of the dashboard.

## Roles

| Role | Access |
|---|---|
| `admin` | Everything: the Users page, all settings, providers, virtual keys, and logs. Grants are implied (the column reads "All grants"). |
| `user` | Only the pages allowed by their `grants` list. No admin pages (Users, most of Settings). |

## Grants

Grants apply to `user` accounts only. An `admin` implies all of them.

| Grant | What it allows |
|---|---|
| `chat` | The Chat and Arena pages and the admin chat endpoints. |
| `usage` | Stats and usage dashboards (read-only). |
| `logs` | Request logs: routing metadata only, never prompt content. |
| `models` | The models list (read-only). |
| `virtual_keys` | The Virtual Keys page, with full CRUD over the user's own keys. |

The grant catalog is defined in `internal/user/grants.go`; add a row there when a new alert- or feature-worthy surface is introduced.

## Managing users

Admins manage accounts from the Users page:

- **Create**: choose a username, display name, email, role, and grants, then set an initial password (minimum 8 characters). Share that password with the user out of band; the user signs in with it and the flow proceeds as normal.
- **Edit**: change profile fields, role, or grants, and enable or disable the account.
- **Reset password**: set a new password for the user.
- **Reset second factor**: clear a user's TOTP enrollment if they lose their authenticator.
- **Delete**: remove the account entirely.

The table shows each account's role, grants, enabled/disabled status (a shield icon marks accounts with a confirmed TOTP second factor), and last-login time.

## Per-user rate limits

Each account can carry optional RPS, burst, and TPM caps. These are **aggregate** limits: they bound the combined proxy traffic across every virtual key that user owns, on top of the per-key limits on individual keys. A null value means no cap. This lets you hand a user several keys and still bound their total consumption. See [Configuration](Configuration) for how the per-key and per-user buckets interact.

## Login and second factor

- A user signs in with username and password on the standard login screen. A "Sign in with password" block appears alongside passkey, SSO, and the admin token once any user exists.
- If the user has enrolled TOTP, the login completes with their own 6-digit code (separate from the admin TOTP). Recovery codes are per account.
- Sessions are SHA-256 hashed and never stored in plaintext, on the same infrastructure passkey and admin TOTP login use.
- The admin token, passkeys, and SSO all keep working regardless of how many user accounts exist.

## Security notes

- Passwords are hashed with **argon2id** (per-account random salt, PHC string format), the same KDF used for `MASTER_KEY` derivation. Plaintext passwords are never stored.
- User TOTP secrets are AES-256-GCM encrypted at rest with `MASTER_KEY`, like provider keys.
- Grants are enforced server-side on every request. The UI gating is convenience, not the security boundary: a user who loses a grant cannot reach that data even by calling the API directly.
- In a High Availability fleet, user accounts live in each member's database and are replicated by Front Desk config-sync (alongside providers, virtual keys, settings, and failover groups), so a user can sign in against any healthy member. See [High Availability](High-Availability).
