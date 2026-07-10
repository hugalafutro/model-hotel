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
- **Admin-token sync and reset** - push one instance's admin token to every member, or rotate the whole fleet's token at once, with a preview and double-confirm before any overwrite.
- **Control-plane event log** - a filterable record of membership and health transitions.
- **Passkey and TOTP login** - protect the dashboard with a FIDO2/WebAuthn passkey (Touch ID, Windows Hello, YubiKey) and/or an authenticator-app second factor, on top of the login token.

## Image details

- Self-contained: stores everything in an embedded SQLite database under `DATA_DIR` (no Postgres). Mount a volume at `/data` to persist members, settings, and credentials.
- Listens on **port 8090** (admin UI plus the internal `/traefik/config` endpoint).
- Runs as a non-root user, base packages upgraded for current security patches.
- `linux/amd64`.

## Quick start

Front Desk is meant to be deployed as part of the ready-made HA stack (Traefik + Front Desk), not on its own. Copy the [`deploy/ha/`](https://github.com/hugalafutro/model-hotel/tree/master/deploy/ha) directory, fill in `.env` (see [`.env.example`](https://github.com/hugalafutro/model-hotel/blob/master/deploy/ha/.env.example)), and:

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
| `FRONTDESK_MASTER_KEY` | ✅ | AES-256-GCM key that encrypts member admin tokens and the TOTP secret at rest. Independent of any member's `MASTER_KEY`. Rotating it makes stored tokens unrecoverable (re-enter them in the UI). |
| `FRONTDESK_TOKEN` | optional | Dashboard login secret. Leave blank to auto-generate one, printed once to the logs on first boot. |
| `FRONTDESK_TRUSTED_PROXIES` | optional | External reverse-proxy address(es) (CIDR, comma-separated) trusted for `X-Forwarded-*` (real client IP and HTTPS detection). |
| `LB_PORT` | optional | Host port for client traffic (Traefik). Default `8080`. |
| `FRONTDESK_PORT` | optional | Host port for the Front Desk dashboard. Default `8090`. |
| `FRONTDESK_DEBUG_LOG` | optional | Verbose structured logging. Default `false`. |

## Security and privacy

Member admin tokens and the TOTP secret are encrypted at rest with AES-256-GCM using `FRONTDESK_MASTER_KEY`. Login session tokens and TOTP recovery codes are SHA-256 hashed, never stored in plaintext. Front Desk carries the same no-prompt-logging guarantee as Model Hotel: it never sees or stores `/v1` request content, because it is never in the request path.

## Full documentation

- [High Availability guide](https://github.com/hugalafutro/model-hotel/wiki/High-Availability) - the complete Front Desk + Traefik walkthrough
- [Model Hotel on Docker Hub](https://hub.docker.com/r/hugalafutro/model-hotel) - the gateway image this control plane manages
- [Project README](https://github.com/hugalafutro/model-hotel#readme)

## License

[MIT](https://github.com/hugalafutro/model-hotel/blob/master/LICENSE).
