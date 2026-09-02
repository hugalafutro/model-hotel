# 👥 Multi-User Access

Model Hotel ships with a single shared admin token by default. Multi-user access adds named dashboard accounts on top of that: each user signs in with a username and password (plus their own optional TOTP second factor) on the same login screen, and is scoped to exactly the parts of the dashboard they need.

<p align="center">
<img src="screenshots/users.png" alt="Users page" width="760"><br>
<em>The Users page: one admin and several scoped user accounts, with role, grants, status, and last-login columns</em>
</p>

## Overview

- Named accounts live in the `users` table, separate from the env admin token.
- Two roles: **admin** (full access, every grant implied) and **user** (access bounded by a grant list).
- A user **owns** their virtual keys. Per-account rate limits (RPS / burst / TPM) cap that account's aggregate traffic: every virtual key the user owns, plus what they send from the dashboard Chat and Arena pages.
- An account can also carry a **provider cap**, an admin-set list of the providers it may reach. The cap bounds every key that account owns.
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

## Provider access

Each account can carry an optional **provider cap**: the list of providers that account is allowed to reach. The default is no cap, meaning every provider, and every account that existed before the cap was introduced keeps that default, so an upgrade changes nothing until you set one.

Admins set it on the Users page alongside role and grants. The **Provider access** column reports the account's state at a glance: "All providers" for an uncapped account, the first allowed provider plus a count of the rest for a capped one, and "No providers" for an account whose cap has been emptied.

The cap bounds every virtual key that account owns. Enforcement is an **intersection at request time**: on each request the key's own `allowed_providers` list is intersected with the owner's cap, and the request may route only to a provider present in both. Narrowing a cap therefore takes effect on the very next request, with no need to edit or re-issue any of the account's existing keys. The same cap applies on the dashboard Chat and Arena pages, which carry no virtual key at all, so there the account cap is the whole rule.

Three consequences are worth knowing before setting one:

- **A capped account does not automatically receive newly added providers.** The cap names providers explicitly, so adding a provider to Model Hotel immediately widens what every uncapped account can reach and changes nothing for a capped one. Each capped account that should have the new provider must be granted it. This is what the Provider access column is for: an account quietly falling behind the provider list is otherwise invisible.
- **Deleting the last provider in an account's cap leaves that account able to reach nothing.** Deleting a provider strips it from every account cap and every key allow-list that named it. An account whose cap held only that provider ends up with an empty cap, and an empty cap means exactly zero providers, never all of them. The Users page shows this state explicitly as "No providers" rather than letting it read as unrestricted, and the affected keys are marked on the Virtual Keys page too.
- **Admins are bound by the cap at write time as well.** Creating or updating a virtual key that names a provider outside its owner's cap is refused, and the refusal names both the provider and the account. An admin who wants a key to reach a provider raises the owner's cap first, then sets it on the key. Creating a key for a capped account without naming any provider stores the owner's cap on the key, which is what "everything this account may reach" means in practice.

## Per-user rate limits

Each account can carry optional RPS, burst, and TPM caps. These are **aggregate** limits: they bound that account's combined traffic, on top of the per-key limits on individual keys. A null value means no cap. This lets you hand a user several keys and still bound their total consumption. See [Configuration](Configuration) for how the per-key and per-user buckets interact.

The account bucket covers **every** surface the account can send from, not only its virtual keys. Requests made from the dashboard Chat and Arena pages carry no virtual key, and they are charged to the same bucket. So a user who exhausts an aggregate cap sees the Chat page fail with `429 Too Many Requests` carrying `user rate limit exceeded` (the RPS/burst cap) or `user token rate limit exceeded` (the TPM cap), exactly as their API traffic would. Set these caps expecting them to bound the person, not only the keys they hold.

## Login and second factor

- A user signs in with username and password on the standard login screen. A "Sign in with password" block appears alongside passkey, SSO, and the admin token once any user exists.
- If the user has enrolled TOTP, the login completes with their own 6-digit code (separate from the admin TOTP). Recovery codes are per account.
- Sessions are SHA-256 hashed and never stored in plaintext, on the same infrastructure passkey and admin TOTP login use.
- The admin token, passkeys, and SSO all keep working regardless of how many user accounts exist.

## Audit trail

Every mutating dashboard action (POST, PUT, PATCH, DELETE on the authenticated `/api/*` surface) by any signed-in account, admin or user, is recorded in an audit trail. Two kinds of call are left out because they are not admin actions: the fleet heartbeat Front Desk sends every member, and a read-only POST that answers a question without changing anything (the backup prune preview). Viewing the trail is admin-only.

<p align="center">
<a href="screenshots/audit.png"><img src="screenshots/audit.png" alt="Audit page with the detail modal open" width="760"></a><br>
<em>The Audit page: who changed what, with the detail modal open on a model-test entry</em>
</p>

Each entry records who (actor and role), what (HTTP method and route pattern), the target entity, the response status, the caller's remote address, and when. Request and response bodies are never stored, so secrets (provider keys, passwords) can never end up in the trail.

- The **Entity** column resolves the target's UUID to its current display name at read time (model, provider, virtual key, failover group, or username). Names are never stored: after a rename the trail shows the new name, and a deleted entity leaves only its UUID as the trace.
- Clicking a row opens a detail modal with copyable fields: full timestamp, actor, entity name and UUID, endpoint pattern, the concrete request path, and remote address.
- The list filters by actor and method, and paginates newest-first with a cursor.
- The trail can be purged from the page. The purge is itself a mutating request and is recorded, so a wiped trail always shows who wiped it.

See [API Reference](API-Reference#audit-trail) for the `/api/audit` endpoints.

## Security notes

- Passwords are hashed with **argon2id** (per-account random salt, PHC string format), the same KDF used for `MASTER_KEY` derivation. Plaintext passwords are never stored.
- User TOTP secrets are AES-256-GCM encrypted at rest with `MASTER_KEY`, like provider keys.
- Grants are enforced server-side on every request. The UI gating is convenience, not the security boundary: a user who loses a grant cannot reach that data even by calling the API directly.
- In a High Availability fleet, user accounts live in each member's database and are replicated by Front Desk config-sync (alongside providers, virtual keys, settings, and failover groups), so a user can sign in against any healthy member. See [High Availability](High-Availability).
