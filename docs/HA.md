# High Availability: Front Desk + Traefik

Run two or more independent Model Hotel installations behind a single client
endpoint, with zero client-side change. This is the "Front Desk" HA stack: a
Traefik v3 **data plane** that carries traffic, and a small **Front Desk**
control-plane app where you manage membership in a browser.

Front Desk is **never in the request path**. If it stops, Traefik keeps serving
with the last config it fetched; only membership changes pause until it returns.

```
        clients ─▶ TLS proxy (https) ─▶ Traefik :8080 ─┬─▶ hotel-1 (ip1:8081)
                                                        └─▶ hotel-2 (ip2:8081)
                                          ▲ polls /traefik/config every ~5s
                                          │
                                    Front Desk :8090  ◀── you, in a browser
```

## What you deploy

Everything is in [`deploy/ha/`](../deploy/ha/):

- `docker-compose.yml` - Traefik + Front Desk, two containers.
- `.env.example` - copy to `.env` and fill in the secrets.

Each Model Hotel member (app + its own Postgres) stays a normal, independent
install on its own host and update schedule. The HA stack does not touch them.

## Prerequisites

- **A TLS-terminating reverse proxy in front of the stack.** Ingress is
  HTTPS-only. This stack speaks plain HTTP internally; an external proxy (e.g.
  nginx with a real certificate) terminates TLS for both published ports. There
  is no plain-HTTP ingress path. Passkeys require HTTPS, and they work the
  moment TLS is in front.
- **The same `MASTER_KEY` on every member** (see "Three secrets" below).
- **`TRUSTED_PROXIES` on every member** including the HA host and the edge
  proxy, so per-IP rate limiting and logs see real client IPs.

## Quick start

```bash
cd deploy/ha
cp .env.example .env
# Edit .env: set FRONTDESK_PUBLIC_ORIGIN, FRONTDESK_MASTER_KEY, etc.
docker compose up -d        # or, from the repo root: make ha-up
docker compose logs -f      # capture the generated FRONTDESK_TOKEN if you left it blank
```

Traefik now answers client traffic on `:8080`; Front Desk's UI is on `:8090`.
Point your external TLS proxy at both (see "TLS proxy" below).

## Drop-in migration runbook

You have one instance at `ip1:8080`. Move it aside and let the HA stack take
over `:8080` so clients never change their base URL.

1. On the existing host: change the published port `8080` to `8081`, then
   `docker compose up -d`.
2. Copy `deploy/ha/` to the HA host, fill in `.env`, `docker compose up -d`.
   Traefik now answers on `:8080`; clients work again.
3. In the Front Desk UI: add `http://ip1:8081` as "hotel-1" (supplying its admin
   token), confirm the health badge is green. Front Desk highlights the **first
   member as the default sync primary** and shows a one-time notice that
   hotel-1's admin token will become the whole group's admin token when you
   sync. Tick the acknowledgement once you have it saved.
4. On machine 2: deploy Model Hotel on `:8081` with the **same `MASTER_KEY`** and
   `TRUSTED_PROXIES` including the HA host. The `ADMIN_TOKEN` need not match by
   hand: supply each member's current admin token when adding it, then run
   "sync admin token" to converge them on the primary's.
5. On the primary dashboard: create a backup. On the new instance: restore it.
6. In Front Desk: add `http://ip2:8081` as "hotel-2" (supplying its admin token).
7. **Repeat steps 4-6 for each additional member.** Same `MASTER_KEY` +
   `TRUSTED_PROXIES`, restore the primary's backup, add it with its current admin
   token. Run "sync admin token" once after the members are in to converge them
   all on the primary.
8. Maintenance: drain a member in Front Desk, rebuild it, re-activate. Re-run the
   backup/restore (step 5) after any provider/key/settings change, until
   automated config sync ships.

## Three secrets, three jobs (do not conflate them)

1. **`FRONTDESK_TOKEN`** logs you into the **Front Desk UI**. Its own secret,
   unrelated to any member. Leave it blank in `.env` to auto-generate one printed
   once to the logs on first boot.
2. **A member's `ADMIN_TOKEN`** logs you into **that member's dashboard** (direct
   or through the LB hostname). This is the one that benefits from converging
   across members via the sync flow.
3. **`MASTER_KEY`** is not a login. It is the AES-256-GCM key that decrypts each
   member's provider API keys at rest.

Plus, internal to Front Desk: **`FRONTDESK_MASTER_KEY`** encrypts the member
admin tokens (and the TOTP secret) that Front Desk stores. It is independent of
any member's `MASTER_KEY`.

### `MASTER_KEY` must match across members (required, set by hand)

Backup/restore is raw `pg_dump`/`pg_restore`, so provider keys travel as
ciphertext. A member with a different `MASTER_KEY` restores the rows but cannot
decrypt them, leaving every provider dead there. It is a live decryption secret,
so set it the same way you would a shared DB password: out-of-band, never
auto-transmitted between instances.

### Member admin token: the stored hash is what must agree

`internal/admin` persists the credential as `sha256:<hex>` in
`DATA_DIR/admin-token` (a file, not the DB, so `pg_dump` skips it) and validates
by hash-compare. The file is authoritative; the `ADMIN_TOKEN` env only seeds it
when missing and is ignored once the file exists. So Front Desk converges the
member dashboards onto one shared token by syncing that hash, rather than asking
you to hand-match env vars. Matching only matters for dashboard-through-the-LB;
API clients use virtual keys and never see it.

### Recovery footgun

Because the `admin-token` file is authoritative, **editing `.env` and rebuilding
does NOT change an existing member's token** when `DATA_DIR` persists (the normal
case). Use Front Desk's **Reset admin token** action (Settings tab) to mint a
new group token and push it to every member, no in-container file editing. As
long as any one member still holds a token you know, make it the primary and sync
from there: you are never locked out of the whole group at once. The data plane
(`/v1` traffic) is unaffected by any of this; clients use virtual keys.

## Replicating config across the fleet

A fresh member starts empty: no providers, no virtual keys. Rather than
re-entering everything on each instance, replicate one member's configuration to
the rest from Front Desk's **Settings -> Config sync**.

How it works: you pick a **primary** (the config source of truth); Front Desk
pulls its config and pushes it to every other member so the fleet converges. It
is a **separate action** from admin-token sync, on purpose: replacing config can
remove providers or keys on a replica, so it must not ride along with a routine
token rotation. Pick the primary once for both if you like; they stay two
buttons.

What replicates, and what does not:

| Replicated (config) | Stays per-instance |
|---|---|
| Providers (incl. their encrypted keys) | Request logs, metering, events |
| Virtual keys (matched by hash) | Backups, runtime stats |
| Syncable settings (discovery, timeouts, circuit breaker, hedging, backups, retention) | Passkeys / TOTP (auth is per-instance) |
| | Alerting destination (apprise URL/targets) |

Models and failover groups are **not** copied: each member rediscovers models
from the synced providers and re-forms failover groups automatically. (Manual
model overrides such as a custom disable or rename are a planned follow-up.)

Provider keys travel as their stored ciphertext and decrypt on each member
because the fleet shares `MASTER_KEY`. If a member's `MASTER_KEY` differs, Config
sync flags it as **blocked** and writes nothing to it (it could not use the keys
anyway). A virtual key's per-key provider restriction is carried by provider
**name** and re-resolved to each member's own provider IDs.

Runbook:

1. On the primary, set up providers and virtual keys as usual.
2. Front Desk -> Settings -> **Config sync** -> choose the primary. The preview
   lists, per member, what will be added, updated, or removed (and flags any
   blocked member). Anything the primary does not have is **removed** from a
   replica that has it, so review the preview before confirming.
3. Confirm. Each member is independent: a failure leaves that member untouched
   and is reported; re-run to retry. Request logs and metering are never touched
   (a removed provider's logs are kept, with the provider link nulled).

## TLS proxy

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

## Observability

The Traefik access log is off by default to avoid logging request lines.
Front Desk's Events tab records control-plane facts only (membership changes,
health transitions tagged by source, config lifecycle, and a warning when
**Traefik has not polled for too long** - the one silent failure mode of the
HTTP-provider design). No request or prompt content is ever logged.

## What this does and does not give you

- **Bounded loss.** Unplanned death of a member loses only its in-flight
  requests; Traefik retries not-yet-streamed failures onto a healthy member.
- **Zero-loss planned maintenance.** Drain a member: established streams finish,
  new requests go elsewhere.
- **Not** Postgres HA, **not** LB redundancy: the HA host and each member's
  Postgres remain single points of failure for their own scope (accepted at
  homelab scale). No automated cross-instance config sync yet, so keep config in
  step with backup/restore and runbook discipline.

## Acceptance checks (two machines)

1. Drop-in swap (runbook 1-3); client traffic uninterrupted after step 2.
2. Kill member 1 mid-stream: that stream breaks, retry lands on member 2, badge
   goes red within seconds.
3. Drain member 2 during a long stream: the stream completes, no new requests
   arrive; rebuild, re-activate, badge green; browser SSE reconnects.
4. A virtual key created on member 1 and backup-restored to member 2
   authenticates on both.
5. Token (and, with parity enabled, passkey/TOTP) login works through the proxy.
6. Events carry correct source attribution; the "Traefik stopped polling"
   warning fires when Traefik is stopped while Front Desk runs.
