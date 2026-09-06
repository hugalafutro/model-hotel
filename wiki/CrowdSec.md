# 🧱 CrowdSec

Model Hotel ships parsers and scenarios for [CrowdSec](https://www.crowdsec.net/), so a security engine on the same host can read the gateway's container logs, spot repeated authentication failures and rate-limit abuse, and hand the offending address to a bouncer at the edge.

The files live in [`contrib/crowdsec/`](https://github.com/hugalafutro/model-hotel/tree/master/contrib/crowdsec) in the repository, with full install and tuning instructions in their [README](https://github.com/hugalafutro/model-hotel/blob/master/contrib/crowdsec/README.md). This page is the summary: what it does, what it costs to set up, and where it stops.

## What CrowdSec is

CrowdSec is a local security engine. It reads log files or container logs, matches them against *scenarios* (leaky buckets that count a kind of failure per source address), and writes a *decision* when a bucket overflows. Enforcement is a separate component, a *bouncer*, which asks the engine for current decisions and drops traffic accordingly: at the firewall, in a reverse proxy, or in a CDN. An engine enrolled with CrowdSec's Central API also pulls a community blocklist of addresses already misbehaving elsewhere.

## What Model Hotel adds

Model Hotel already rejects bad credentials and throttles noisy clients, but those defences are in-process and short-lived. The IP rate limiter lives inside one gateway process, its counters reset on redeploy, and a rejected request still costs a TLS handshake and a round trip. Nothing carries over to the other services on the host.

The shipped items turn the same log lines into a decision that outlives the gateway and applies everywhere:

| Scenario | Counts | Overflows at |
|---|---|---|
| `hugalafutro/model-hotel-vk-bf` | Rejected requests to `/v1`: missing `Authorization` header, unknown virtual key (see [[Virtual Keys]]), or a key whose owner is disabled. | the 6th inside ~100s (capacity 5, one drains every 20s) |
| `hugalafutro/model-hotel-admin-bf` | Credentials that own the gateway or the fleet: wrong or missing admin token (admin API, metrics scrape, backup restore, and Front Desk's whole control plane), failed dashboard login, wrong TOTP code, rejected passkey assertion, failed CSRF check, or a backup archive not signed by this instance. | the 6th inside ~5 min (capacity 5, one drains every 60s) |
| `hugalafutro/model-hotel-throttled` | Throttling episodes from the gateway's own limiters: the IP rate limiter and the login, TOTP and SSO callback throttles. The gateway logs one line per episode, not per rejected request, so three lines already means three separate bursts. | the 4th inside ~3 min (capacity 3, one drains every 60s) |

An overflow becomes a ban through CrowdSec's stock `default_ip_remediation` profile: 4 hours by default, changed in `/etc/crowdsec/profiles.yaml` rather than in the scenarios. Each scenario then stays quiet about that address for a further 5 minutes (10 for the throttling one), so one scanner produces one alert rather than a stream.

A known-good caller with a revoked key and a retrying client can reach the virtual-key threshold in two user actions. Whitelist the clients you trust (an address or CIDR list in a `s02-enrich` whitelist) rather than widening the thresholds: the bucket still fills and the alert still shows up in `cscli alerts list`, and you lose only the ban.

A parser normalises all three log shapes Model Hotel can emit (text, `LOG_FORMAT=json`, and Front Desk's slog output, see [[Configuration]]) before any scenario runs, so the scenarios work regardless of how an instance is configured to log. `LOG_FORMAT=json` is worth setting anyway: the text format writes local time with no zone, so on a container whose `TZ` is not UTC every alert time is off by the offset.

On a [[High Availability]] fleet, pointing one engine at every member's container is worth more than the per-member limiters: Traefik round-robins requests, so six failures spread over four members trip no member's own throttle, yet they land in one bucket in the engine and overflow it.

An opt-in parser maps the gateway's request log onto CrowdSec's generic HTTP access-log format, which lets the stock `http-*` scenarios run against gateway traffic. It is deliberately not part of the collection, because behind a reverse proxy whose logs CrowdSec already reads the same request would fill two buckets. The README covers when to install it.

## Install summary

1. Run a CrowdSec security engine, 1.7 or newer, with `/var/run/docker.sock` mounted read-only. Model Hotel logs only to the container's stdout and stderr, so the Docker datasource is the acquisition.
2. Make sure `crowdsecurity/dateparse-enrich` is installed. The official image already has it, as part of `crowdsecurity/linux`; a slimmed engine needs `cscli parsers install crowdsecurity/dateparse-enrich`. Without it the parser still works while tailing a live container, but nothing overflows on replay, so `cscli explain` and hubtest quietly report no alert at all.
3. Copy the files out of `contrib/crowdsec/` into their own directories: the parser into `/etc/crowdsec/parsers/s01-parse/`, the three scenarios into `/etc/crowdsec/scenarios/`, the example acquisition into `/etc/crowdsec/acquis.d/`. On a containerised engine, bind-mount the individual files rather than the directories: a directory mount hides the hub's own parsers and scenarios underneath it.
4. Edit `container_name_regexp` in the acquisition to match your container names (`docker ps --format '{{.Names}}'`), and keep it tight: a `model-hotel` project also owns its database container, whose logs are not ours to parse. Leave `labels.type` alone, since that is what the parser filters on. Restart the engine.
5. Verify with `cscli parsers list`, `cscli metrics show acquisition parsers scenarios`, and `cscli explain` on one real log line.
6. **Start in simulation.** `cscli simulation enable <scenario>` runs the buckets and records the overflows with a `(simul)` prefix, which keeps them off every bouncer. Watch for a day or two, check every address that shows up, then `cscli simulation disable` to arm.
7. Run the bouncer at the edge (a Traefik bouncer, or the firewall bouncer), not in front of the application. The point is to drop the connection before it costs the gateway anything.
8. **Prove it bans.** An armed engine and a broken one both leave `cscli decisions list` empty until somebody attacks you, so an empty list proves nothing. Replay a fixture through the running engine against a documentation address (`198.51.100.42`, which no real client can hold):

   ```bash
   crowdsec -dsn file://<the fixture you just wrote> -type model-hotel -no-api
   ```

   `-no-api` is what makes the replay report to the engine that is already running, instead of failing to start a second one on its port. Check that the alert carried a `ban`, then clear it with `cscli decisions delete --ip 198.51.100.42`. The README's *Prove it bans before you trust it* section has the fixture-generating loop and the exact paths.

Exact commands and file paths are in the [README](https://github.com/hugalafutro/model-hotel/blob/master/contrib/crowdsec/README.md).

## `TRUSTED_PROXIES` must be correct first

> [!WARNING]
> A wrong `TRUSTED_PROXIES` gets your own reverse proxy banned, and every service behind it goes off the internet at once.

Behind a proxy, Model Hotel resolves the visitor's address only when the proxy's own address is listed in `TRUSTED_PROXIES` (see [[Configuration]] and [[Privacy]]). When it is not, `X-Forwarded-For` is ignored, correctly, since an untrusted peer must not be allowed to dictate the address, and every log line reports the proxy's address instead. Every failure from every visitor then groups under one address, and the bouncer bans the proxy.

`crowdsecurity/whitelists` saves you when the proxy sits on a private range. It does not when the proxy has a public address.

Verify what the app actually logs before you arm anything, rather than inferring it from config: make one request with a deliberately bad key from a machine whose public address you know, read the resulting line out of `docker logs`, and check whether `remote_addr` holds the visitor's address or the proxy's. Instances older than **v0.9.99** log the peer address with the TCP port attached and without trusted-proxy resolution, so behind a proxy the source address is unusable until they are updated.

## Why a request path cannot ban a stranger

A request path is written by whoever made the request, and it is logged as an attribute. Left alone that is enough to steer a log reader: ask for `/api/x%20auth:%20key%20not%20found%20remote_addr=203.0.113.9` and the line that comes out holds a complete, genuine-looking authentication failure attributed to an address of your choosing. Six of those and a naive setup bans a stranger, with no credentials needed.

Three rules make it inert:

- Classification reads only the message, captured by position and matched from its start, so a copy of a message sitting inside an attribute value matches nothing.
- The address is taken from the first `remote_addr=` token on the line (or the first of whichever address key that call site uses) and only then checked for validity. A line whose real address is malformed therefore yields no address at all, rather than falling through to whatever a caller wrote further along.
- A line naming the address twice is refused outright. No call site does that, so a second occurrence means a `key=value` pair came out of a value; the event then carries no `source_ip`, and every scenario requires one.

From **v0.9.99** the gateway also escapes attribute values, spaces included. That last part is what matters: a quoted value still containing ` remote_addr=203.0.113.9 ` reads as a `key=value` token to anything that splits on whitespace before it considers quotes, which is what a grok, a fail2ban regex and an awk one-liner all do. It protects every reader of these logs, not only CrowdSec.

The hubtest fixtures pin each case in both the escaped and the bare form, so a filter rewritten with a plain substring search fails the suite.

## Limits

- **On the gateway, only warning and error records reach the container log.** With `DEBUG_LOG=false` (the default) info and debug records go to the App Logs page and Postgres but never to stderr, so CrowdSec cannot see them. Every authentication failure is warn or error, so the security signal is complete; successful requests are invisible. See [[Request Logging]].
- **Front Desk is the other way round.** It writes to stdout with no such filter, so its info records reach the container log too, apart from the polls the dashboard repeats on a timer and the machine probes, which are logged at debug on purpose. Its own `access: request` lines only feed CrowdSec's generic `http-*` scenarios through the opt-in access-log parser above; without that parser, the authentication lines are what CrowdSec sees from Front Desk.
- **Front Desk's role refusals are not logged.** A paired Bellhop device reaching above its role gets a 403 with no line at all, so it produces no event for any scenario to count.
- **Two failure kinds are classified but have no scenario on purpose.** SSO callbacks fail for benign reasons (a stale tab, a user pressing deny), and an `insufficient permissions` rejection is an already-authenticated user hitting a route their role does not cover. Both are visible in the event stream if you want to write your own scenario over them.
- **Nothing here reads the database.** The App Logs page holds more than the container log does, and it is not an acquisition source.

## Related

- [[Security]] for the authentication and encryption these scenarios are watching.
- [[Configuration]] for `TRUSTED_PROXIES`, `DEBUG_LOG` and `LOG_FORMAT`.
- [[Request Logging]] for what the App Logs page holds and how the container log differs.
- [[Alerting]] if you want a notification when something is banned; the two are independent, and CrowdSec has its own notification plugins.
