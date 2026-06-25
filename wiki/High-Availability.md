# 🏨 High Availability: Front Desk + Traefik

Run two or more independent Model Hotel installations behind a single client
endpoint, with no client-side change. This is the **Front Desk** HA stack: a
Traefik v3 **data plane** that carries traffic, and a small **Front Desk**
control-plane app where you manage membership in a browser.

Front Desk is **never in the request path**. If it stops, Traefik keeps serving
with the last config it fetched; only membership changes pause until it returns.

<!-- TODO screenshot: Front Desk Members tab with two healthy members -->

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [What You Deploy](#what-you-deploy)
3. [Prerequisites](#prerequisites)
4. [Quick Start](#quick-start)
5. [Drop-in Migration Runbook](#drop-in-migration-runbook)
6. [Three Secrets, Three Jobs](#three-secrets-three-jobs)
7. [Admin Authentication (Passkeys & TOTP)](#admin-authentication-passkeys--totp)
8. [TLS Proxy](#tls-proxy)
9. [Observability](#observability)
10. [What This Does and Does Not Give You](#what-this-does-and-does-not-give-you)
11. [Acceptance Checks](#acceptance-checks)

---

## Architecture Overview

```
        clients ─▶ TLS proxy (https) ─▶ Traefik :8080 ─┬─▶ hotel-1 (ip1:8081)
                                                        └─▶ hotel-2 (ip2:8081)
                                          ▲ polls /traefik/config every ~5s
                                          │
                                    Front Desk :8090  ◀── you, in a browser
```

- **Traefik (data plane)** carries all client traffic and load-balances across
  members. It pulls its routing config from Front Desk over the internal compose
  network via Traefik's HTTP provider, polling `GET /traefik/config` every ~5s.
- **Front Desk (control plane)** is a small Go binary with an embedded SQLite
  database and its own web UI. You add, drain, and remove members here, sync
  admin tokens, and watch health. It is **never** in the request path.
- **Members** are normal, independent Model Hotel installs (app + their own
  Postgres), each on its own host and update schedule. The HA stack does not
  touch them beyond reading health/version and pushing the admin-token hash.

Because Traefik caches the last config it fetched, a Front Desk outage degrades
gracefully: traffic keeps flowing, and only membership changes wait.

---

## What You Deploy

Everything lives in
[`deploy/ha/`](https://github.com/hugalafutro/model-hotel/tree/master/deploy/ha):

- `docker-compose.yml` - Traefik + Front Desk, two containers.
- `.env.example` - copy to `.env` and fill in the secrets.

The repo also ships [`docs/HA.md`](https://github.com/hugalafutro/model-hotel/blob/master/docs/HA.md)
with the same runbook in source form.

---

## Prerequisites

- **A TLS-terminating reverse proxy in front of the stack.** Ingress is
  HTTPS-only. This stack speaks plain HTTP internally; an external proxy (nginx,
  Caddy, nginx-proxy-manager, etc.) terminates TLS for both published ports.
  There is no plain-HTTP ingress path. Passkeys require HTTPS and work the moment
  TLS is in front.
- **The same `MASTER_KEY` on every member** (see
  [Three Secrets](#three-secrets-three-jobs)).
- **`TRUSTED_PROXIES` on every member**, including the HA host and the edge
  proxy, so per-IP rate limiting and logs see real client IPs.

---

## Quick Start

```bash
cd deploy/ha
cp .env.example .env
# Edit .env: set FRONTDESK_PUBLIC_ORIGIN, FRONTDESK_MASTER_KEY, etc.
docker compose up -d        # or, from the repo root: make ha-up
docker compose logs -f      # capture the generated FRONTDESK_TOKEN if you left it blank
```

Traefik answers client traffic on `:8080`; Front Desk's UI is on `:8090`. Point
your external TLS proxy at both (see [TLS Proxy](#tls-proxy)).

---

## Drop-in Migration Runbook

You have one instance at `ip1:8080`. Move it aside and let the HA stack take over
`:8080` so clients never change their base URL.

1. On the existing host: change the published port `8080` to `8081`, then
   `docker compose up -d`.
2. Copy `deploy/ha/` to the HA host, fill in `.env`, `docker compose up -d`.
   Traefik now answers on `:8080`; clients work again.
3. In Front Desk: add `http://ip1:8081` as "hotel-1" (supplying its admin token),
   confirm the health badge is green. Front Desk highlights the **first member as
   the default sync primary** and shows a one-time notice that hotel-1's admin
   token becomes the whole group's admin token when you sync.
4. On machine 2: deploy Model Hotel on `:8081` with the **same `MASTER_KEY`** and
   `TRUSTED_PROXIES` including the HA host. The `ADMIN_TOKEN` need not match by
   hand: supply each member's current admin token when adding it, then run "sync
   admin token" to converge them on the primary's.
5. On the primary dashboard: create a backup. On the new instance: restore it.
6. In Front Desk: add `http://ip2:8081` as "hotel-2" (supplying its admin token).
7. **Repeat steps 4-6 for each additional member.** Run "sync admin token" once
   after the members are in to converge them all on the primary.
8. Maintenance: drain a member in Front Desk, rebuild it, re-activate. Re-run the
   backup/restore (step 5) after any provider/key/settings change, until
   automated config sync ships.

<!-- TODO screenshot: add-member dialog / sync preview modal -->

---

## Three Secrets, Three Jobs

Do not conflate these:

1. **`FRONTDESK_TOKEN`** logs you into the **Front Desk UI**. Its own secret,
   unrelated to any member. Leave it blank in `.env` to auto-generate one printed
   once to the logs on first boot.
2. **A member's `ADMIN_TOKEN`** logs you into **that member's dashboard** (direct
   or through the LB hostname). This is the one the sync flow converges across
   members.
3. **`MASTER_KEY`** is not a login. It is the AES-256-GCM key that decrypts each
   member's provider API keys at rest.

Plus, internal to Front Desk: **`FRONTDESK_MASTER_KEY`** encrypts the member
admin tokens (and Front Desk's own TOTP secret) that Front Desk stores. It is
independent of any member's `MASTER_KEY`.

### `MASTER_KEY` must match across members

Backup/restore is raw `pg_dump`/`pg_restore`, so provider keys travel as
ciphertext. A member with a different `MASTER_KEY` restores the rows but cannot
decrypt them, leaving every provider dead there. It is a live decryption secret:
set it out-of-band, the same way you would a shared DB password, never
auto-transmitted between instances.

### Member admin token: the stored hash is what agrees

`internal/admin` persists the credential as `sha256:<hex>` in
`DATA_DIR/admin-token` (a file, not the DB, so `pg_dump` skips it) and validates
by hash-compare. The file is authoritative; the `ADMIN_TOKEN` env only seeds it
when missing. Front Desk converges the member dashboards onto one shared token by
syncing that hash, rather than asking you to hand-match env vars. Matching only
matters for dashboard-through-the-LB; API clients use virtual keys and never see
it.

### Recovery

Because the `admin-token` file is authoritative, editing `.env` and rebuilding
does **not** change an existing member's token when `DATA_DIR` persists (the
normal case). Use Front Desk's **Reset admin token** action (Settings tab) to
mint a new group token and push it to every member, with no in-container file
editing. As long as any one member still holds a token you know, make it the
primary and sync from there: you are never locked out of the whole group at once.
The data plane (`/v1` traffic) is unaffected; clients use virtual keys.

---

## Admin Authentication (Passkeys & TOTP)

Front Desk's own login supports a raw token (`FRONTDESK_TOKEN`), and optionally a
**passkey** (WebAuthn) and **authenticator-app TOTP**, managed under Front Desk's
**Settings → Security**. Passkeys require the stack to be reached over HTTPS,
which the external TLS proxy provides.

Two things are worth understanding about authentication in an HA deployment:

- **Passkeys and TOTP are per-instance and are never synced.** The admin-token
  sync and reset flows push **only** the admin-token hash to each member; they do
  not read, write, or transfer WebAuthn credentials or TOTP secrets. Each member
  keeps its own in its own Postgres, and Front Desk keeps its own in its own
  SQLite. This is by design: a passkey is bound to an origin (its relying-party
  ID is the hostname), so a credential created for one origin would not validate
  against another anyway. Register a passkey on each surface you actually log in
  to.
- **The passkey button only appears once a passkey exists.** A freshly
  provisioned instance shows token (and TOTP, if enabled) login but not a passkey
  button, because no credential is registered yet. Register one under Settings →
  Security and the button appears on the next login.

<!-- TODO screenshot: Front Desk Settings → Security (passkeys + TOTP) -->

---

## TLS Proxy

Put a real TLS proxy in front of both published ports. Example nginx, two
hostnames:

```nginx
# Client traffic: the /v1 API and member dashboards via the LB hostname.
server {
    listen 443 ssl;
    server_name hotel.example.com;
    # ssl_certificate / ssl_certificate_key ...
    location / {
        proxy_pass http://HA_HOST:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;          # keep streaming/SSE alive
        proxy_buffering off;
    }
}

# Front Desk admin UI: a separate hostname.
server {
    listen 443 ssl;
    server_name frontdesk.example.com;
    # ssl_certificate / ssl_certificate_key ...

    # Defense in depth: /traefik/config is unauthenticated (Traefik fetches it
    # over the compose network and it carries no secrets, only member URLs and
    # settings). Do not expose it through the public proxy.
    location = /traefik/config { return 404; }
    location /traefik/ { return 404; }

    location / {
        proxy_pass http://HA_HOST:8090;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_buffering off;
    }
}
```

Set `FRONTDESK_PUBLIC_ORIGIN=https://frontdesk.example.com` and
`FRONTDESK_TRUSTED_PROXIES` to the proxy's address in `.env`.

---

## Observability

The Traefik access log is off by default to avoid logging request lines. Front
Desk's **Events** tab records control-plane facts only: membership changes,
health transitions tagged by source, config lifecycle, and a warning when
**Traefik has not polled for too long** (the one silent failure mode of the
HTTP-provider design). No request or prompt content is ever logged.

---

## What This Does and Does Not Give You

- **Bounded loss.** Unplanned death of a member loses only its in-flight
  requests; Traefik retries not-yet-streamed failures onto a healthy member.
- **Zero-loss planned maintenance.** Drain a member: established streams finish,
  new requests go elsewhere.
- **Not** Postgres HA, **not** LB redundancy: the HA host and each member's
  Postgres remain single points of failure for their own scope (accepted at
  homelab scale). There is no automated cross-instance config sync yet, so keep
  config in step with backup/restore and runbook discipline.

---

## Acceptance Checks

1. Drop-in swap (runbook 1-3); client traffic uninterrupted after step 2.
2. Kill member 1 mid-stream: that stream breaks, retry lands on member 2, badge
   goes red within seconds.
3. Drain member 2 during a long stream: the stream completes, no new requests
   arrive; rebuild, re-activate, badge green; browser SSE reconnects.
4. A virtual key created on member 1 and backup-restored to member 2
   authenticates on both.
5. Token (and, where registered, passkey/TOTP) login works through the proxy.
6. Events carry correct source attribution; the "Traefik stopped polling"
   warning fires when Traefik is stopped while Front Desk runs.
