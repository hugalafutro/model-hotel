# Model Hotel - Front Desk

[github.com/hugalafutro/model-hotel](https://github.com/hugalafutro/model-hotel)

**The High Availability control plane for [Model Hotel](https://hub.docker.com/r/hugalafutro/model-hotel).**

![Github CI](https://github.com/hugalafutro/model-hotel/actions/workflows/ci.yml/badge.svg) ![Go Version](https://img.shields.io/github/go-mod/go-version/hugalafutro/model-hotel) ![License](https://img.shields.io/github/license/hugalafutro/model-hotel)

> **AI-Assisted Project Disclaimer:**
> Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.

Front Desk turns one or more [Model Hotel](https://hub.docker.com/r/hugalafutro/model-hotel) instances into a horizontally-scalable fleet behind a single client endpoint. It pairs a [Traefik](https://hub.docker.com/_/traefik) data plane (which answers client traffic) with this small control-plane app (which manages membership and generates Traefik's routing config). You add, drain, and remove instances from the Front Desk dashboard, and Traefik follows within seconds.

This image is **only** needed for multi-instance HA. A single Model Hotel instance does not need it.

## Front Desk is never in the request path

Front Desk generates Traefik's dynamic config and serves an admin dashboard. It does **not** proxy `/v1` traffic. If Front Desk goes down, Traefik keeps serving with the last config it fetched: membership changes are paused, but client traffic is unaffected. That separation is the whole point of the design.

## What it does

- **Member management** - register each Model Hotel instance by URL and admin token (verified on add, and de-duplicated so the same instance can't join twice), then drain or remove it from the dashboard. Draining stops new traffic without dropping in-flight requests; the config-sync primary is protected and can't be removed.
- **Traefik dynamic config** - publishes an HTTP-provider endpoint Traefik polls every few seconds, so backend changes apply gracefully (in-flight SSE and streams survive a reload).
- **Health and version polling** - continuously checks each member's health, latency, Traefik backend status, and version, flagging the odd version out when the fleet disagrees.
- **Fleet config sync** - replicate one member's config (providers and their keys, virtual keys, users, failover groups, model toggles, settings) to the rest of the fleet, either from a guided sync wizard or automatically from a designated primary on a background schedule.
- **Provider quota badges** - relays the primary member's provider quota readings to the rest of the fleet, so every member works from the same quota state, and shows them on the dashboard.
- **Backup watchdog** - flags a member whose own scheduled backups have gone stale. Front Desk never creates or restores backups itself; each member backs itself up.
- **Alerts via Apprise** - POSTs short summaries of fleet events (a member going down, a config sync failing) to a stateless [Apprise](https://github.com/caronc/apprise) container that fans them out to Telegram, Discord, email and more. Only event metadata is sent, never request content.
- **Bellhop pairing** - pair the [Bellhop](https://github.com/hugalafutro/model-hotel/wiki/Bellhop) Android app for a pocket view of fleet health, traffic, quota badges and events, plus operator controls.
- **Prometheus metrics** - a `/metrics` endpoint for fleet and member state, scrapeable with a dedicated `FRONTDESK_METRICS_TOKEN`.
- **Control-plane event log** - a filterable record of membership and health transitions.
- **Passkey, TOTP and SSO login** - protect the dashboard with a FIDO2/WebAuthn passkey (Touch ID, Windows Hello, YubiKey) and/or an authenticator-app second factor on top of the login token, or sign in through your own OIDC provider.

## Image details

- Self-contained: stores everything in an embedded SQLite database under `DATA_DIR` (no Postgres). Mount a volume at `/data` to persist members, settings, and credentials.
- Listens on **port 8090** (admin UI plus the internal `/traefik/config` endpoint, lockable to Traefik's own polls via `FRONTDESK_TRAEFIK_TOKEN`).
- Runs as a non-root user, base packages upgraded for current security patches.
- `linux/amd64`.

## Quick start

Front Desk is meant to be deployed as part of the ready-made HA stack (Traefik + Front Desk), not on its own. Copy the [`deploy/ha/`](https://github.com/hugalafutro/model-hotel/tree/master/deploy/ha) directory and fill in `.env` (see [`.env.example`](https://github.com/hugalafutro/model-hotel/blob/master/deploy/ha/.env.example)). That compose builds Front Desk from the repository source, so outside a git checkout switch the `frontdesk` service to the prebuilt image: comment out its `build:` block and uncomment the `image:` line (`ghcr.io/hugalafutro/model-hotel-frontdesk:latest`, or this Docker Hub image, `hugalafutro/model-hotel-frontdesk:latest`). Then:

```bash
docker compose up -d
```

Traefik answers client traffic on `:8080`; Front Desk serves its dashboard on `:8090`. Add your instances in the dashboard and you are live.

> **HTTPS-only ingress:** the stack speaks plain HTTP internally. A TLS-terminating reverse proxy (with a real certificate) **must** sit in front of both published ports so browsers and clients only ever reach the stack over HTTPS. Front Desk refuses to start without a public `https://` origin so a plain-HTTP deploy fails loudly.

## Configuration

Set these in `deploy/ha/.env` (the compose file maps them into the container):

| Variable | Required | Purpose |
|---|---|---|
| `FRONTDESK_PUBLIC_ORIGIN` | ✅ | Public `https://` origin the dashboard is reached at (the TLS proxy's hostname). Also the WebAuthn relying-party ID and expected origin. |
| `FRONTDESK_MASTER_KEY` | ✅ | AES-256-GCM key that encrypts member admin tokens and the TOTP secret at rest. Generate with `openssl rand -base64 32`; Front Desk warns at boot when it is shorter than 32 bytes. Independent of any member's `MASTER_KEY`. Rotating it makes stored tokens unrecoverable (re-enter them in the UI). |
| `FRONTDESK_TOKEN` | optional | Dashboard login secret. Leave blank to auto-generate one, printed once to the logs on first boot. |
| `FRONTDESK_TRUSTED_PROXIES` | optional | External reverse-proxy address(es) (CIDR, comma-separated) trusted for `X-Forwarded-*` (real client IP and HTTPS detection). |
| `LB_PORT` | optional | Host port for client traffic (Traefik). Default `8080`. |
| `FRONTDESK_PORT` | optional | Host port for the Front Desk dashboard. Default `8090`. |
| `FRONTDESK_DEBUG_LOG` | optional | Verbose structured logging. Default `false`. |
| `FRONTDESK_METRICS_TOKEN` | optional | Dedicated bearer token for Prometheus scrapes of `/metrics`. Empty (default) keeps the endpoint behind the dashboard login. |
| `FRONTDESK_TRAEFIK_TOKEN` | optional (recommended) | Shared secret for Traefik's `/traefik/config` polls; the compose feeds it to both sides. Empty (default) leaves the endpoint open to anything that can reach port 8090. Generate with `openssl rand -hex 32`. |
| `LB_TRUSTED_PROXIES` | optional | CIDRs of the TLS proxy in front of `LB_PORT` (comma-separated). When set, Traefik passes that proxy's `X-Forwarded-For` chain through to members so they see the real client. Empty by default. |
| `FRONTDESK_ALLOW_HTTP_MEMBERS` | optional | Set `true` to accept plain `http://` member URLs, which are refused otherwise. Trusted internal networks only. Empty by default. |
| `COOKIE_SECURE` | optional | `Secure` attribute on the `fd_session`/`fd_csrf` login cookies. `always` (default) sends them only over HTTPS, right for this stack's TLS-terminating proxy and for localhost. `auto` sets `Secure` from the request scheme (TLS or `X-Forwarded-Proto: https`). `never` disables it for plain-http LAN access; otherwise the browser drops the cookies and login fails. |

## Security and privacy

Member admin tokens and the TOTP secret are encrypted at rest with AES-256-GCM using `FRONTDESK_MASTER_KEY`. Login session tokens and TOTP recovery codes are SHA-256 hashed, never stored in plaintext. Front Desk carries the same no-prompt-logging guarantee as Model Hotel: it never sees or stores `/v1` request content, because it is never in the request path.

## Full documentation

- [High Availability guide](https://github.com/hugalafutro/model-hotel/wiki/High-Availability) - the complete Front Desk + Traefik walkthrough
- [Model Hotel on Docker Hub](https://hub.docker.com/r/hugalafutro/model-hotel) - the gateway image this control plane manages
- [Project README](https://github.com/hugalafutro/model-hotel#readme)

## License

[MIT](https://github.com/hugalafutro/model-hotel/blob/master/LICENSE).
