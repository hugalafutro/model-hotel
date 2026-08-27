# CrowdSec for Model Hotel

Parsers, scenarios and an example acquisition that let a [CrowdSec](https://www.crowdsec.net/)
security engine read Model Hotel's and Front Desk's container logs, spot repeated authentication
failures and rate-limit abuse, and hand the offending address to a bouncer.

```
model-hotel container logs ──► docker datasource ──► hugalafutro/model-hotel-logs (s01-parse)
                                                              │
                                                              ▼
                                        vk-bf │ admin-bf │ throttled  (leaky buckets)
                                                              │
                                                              ▼
                                              CrowdSec LAPI decision ──► bouncer at the edge
```

## What this buys you

Model Hotel already rejects bad credentials and throttles noisy clients, but those defences are
in-process and short-lived. The IP rate limiter lives inside one gateway process, its counters
reset when the container is redeployed, and a rejected request still costs a TLS handshake and a
round trip. None of it carries over to the other services on the host.

Feeding the same lines to CrowdSec changes three things:

- **The ban moves to the edge.** A decision applies to every service the bouncer protects, not
  just the gateway, and it is enforced before the request reaches the app.
- **It outlives the app.** Decisions are held in CrowdSec's LAPI, so a gateway restart or a
  redeploy does not reset them.
- **You get the community blocklist.** An engine enrolled with the Central API pulls addresses
  that are already scanning other people's hosts, so a large share of the traffic these scenarios
  would otherwise catch never arrives.

On a [High Availability](https://github.com/hugalafutro/model-hotel/wiki/High-Availability) fleet,
point one engine at every member's container. Traefik round-robins requests, so five failures
spread over four members would never trip any single member's own limiter; in one engine they land
in the same bucket, keyed on the source address.

Run the bouncer at the edge (a Traefik bouncer, or the firewall bouncer on the host), not as an
application-level one. The point is to drop the connection before it costs the gateway anything.

## What it detects

| Scenario | Counts | capacity / leakspeed / blackhole |
|---|---|---|
| `hugalafutro/model-hotel-vk-bf` | Rejected requests to `/v1`: a missing `Authorization` header, a virtual key the gateway does not know, a key whose owner is disabled. | 5 / 20s / 5m |
| `hugalafutro/model-hotel-admin-bf` | Credentials that own the gateway: a wrong or missing admin token (admin API, metrics endpoint, backup restore), a failed dashboard login, a wrong TOTP code, a failed CSRF check, a restore archive not signed by this instance. | 5 / 60s / 5m |
| `hugalafutro/model-hotel-throttled` | Throttling episodes from the gateway's own limiters: the IP rate limiter, and the login, TOTP and SSO callback throttles. | 3 / 60s / 10m |

All three are leaky buckets grouped by source address. `capacity` is how many events fit at once,
`leakspeed` is how long one takes to drain, so a bucket overflows on the event after capacity when
they arrive faster than the leak drains. `blackhole` suppresses further alerts for that address for
the stated period, so one scanner produces one alert rather than a stream.

Two counts deserve their thresholds explained:

- **`model-hotel-throttled` counts episodes, not requests.** The gateway logs one line when
  throttling *starts*, not one per rejected request. Three lines therefore already means three
  separate bursts that the in-process limiter had to absorb.
- **`model-hotel-admin-bf` drains three times slower than the virtual-key bucket** (60s against
  20s) because a wrong admin token has no benign explanation the way a stale virtual key does.

The scenario labels carry `remediation: true`, so CrowdSec's stock `default_ip_remediation`
profile turns an overflow into a 4h ban. Change the duration in `/etc/crowdsec/profiles.yaml`, not
in the scenarios.

Two failure kinds are classified by the parser but deliberately have **no** scenario:

- `sso`: an OIDC or GitHub callback that did not complete. Callbacks fail for benign reasons (a
  stale tab, a user pressing deny, a slow redirect), so this is signal for a dashboard, not a ban.
- `forbidden`: an already-authenticated user reaching for something their role does not cover.
  That is a permissions problem, not an intruder.

They are still in the event stream, so you can write your own scenario over them if your
deployment warrants it.

## Requirements

- A CrowdSec security engine, **1.7 or newer**. The files here are verified against 1.7.8.
- The **Docker datasource**, which the official `crowdsecurity/crowdsec` image includes.
- `/var/run/docker.sock` mounted read-only into the engine container.
- `crowdsecurity/dateparse-enrich`, which the official image already installs as part of
  `crowdsecurity/linux`. On a slimmed engine without it, install it explicitly:
  `cscli parsers install crowdsecurity/dateparse-enrich`. Without it the parser still works while
  tailing a live container, but nothing overflows in replay, so `cscli explain` and `cscli hubtest`
  quietly show no alert at all.
- Model Hotel **v0.9.99 or newer**. Older builds log the reverse proxy's address rather than the
  visitor's (see `TRUSTED_PROXIES` below) and do not quote attribute values.

`LOG_FORMAT=json` is recommended, though not required. It carries a real timestamp with a zone,
where the text format writes local time with none, so on a container with a non-UTC `TZ` alert
times are off by the offset. It also gives every attribute its own field, which is the most robust
input this parser can get.

The Docker datasource is the only option. Model Hotel writes to the container's stdout and stderr
and nowhere else, and typical deployments use Docker's `local` log driver, whose on-disk framing a
file acquisition cannot read.

```yaml
# docker-compose.yml, alongside your model-hotel service
crowdsec:
    image: crowdsecurity/crowdsec:latest
    restart: unless-stopped
    volumes:
        - /var/run/docker.sock:/var/run/docker.sock:ro
        - ./crowdsec/acquis.d:/etc/crowdsec/acquis.d
        - ./crowdsec/model-hotel-logs.yaml:/etc/crowdsec/parsers/s01-parse/model-hotel-logs.yaml:ro
        - ./crowdsec/model-hotel-vk-bf.yaml:/etc/crowdsec/scenarios/model-hotel-vk-bf.yaml:ro
        - ./crowdsec/model-hotel-admin-bf.yaml:/etc/crowdsec/scenarios/model-hotel-admin-bf.yaml:ro
        - ./crowdsec/model-hotel-throttled.yaml:/etc/crowdsec/scenarios/model-hotel-throttled.yaml:ro
```

Mount the individual files, not the `s01-parse` and `scenarios` directories: a directory mount
hides the hub's own parsers and scenarios underneath it.

## Install

The items are not on the CrowdSec hub, so they install as local files.

```bash
# from contrib/crowdsec/
cp parsers/s01-parse/model-hotel-logs.yaml  /etc/crowdsec/parsers/s01-parse/
cp scenarios/model-hotel-*.yaml             /etc/crowdsec/scenarios/
cp acquis/model-hotel.yaml                  /etc/crowdsec/acquis.d/

# Only needed on an engine without crowdsecurity/linux:
cscli parsers list | grep dateparse-enrich || cscli parsers install crowdsecurity/dateparse-enrich
```

For a containerised engine, bind-mount those directories (as above) or `docker cp` the files in.
`collections/model-hotel.yaml` is not part of a local install; it exists for hub submission, and a
local collection referring to items the hub does not know will not resolve.

### The acquisition

`acquis/model-hotel.yaml` carries the names the shipped compose files produce, and needs checking
against yours:

```yaml
source: docker
container_name_regexp:
    - "^/?model-hotel-app-1$"
    - "^/?front-desk-frontdesk-1$"
labels:
    type: model-hotel
```

- **`container_name_regexp` must match your container names.** Confirm them with
  `docker ps --format '{{.Names}}'`. Keep the regexp tight: a project called `model-hotel` also owns
  `model-hotel-db-1`, whose logs are not ours to parse. Use the regexp form and not a fixed name or
  id, because a redeploy gives the container a new id and only the regexp re-attaches on its own.
  The Docker API reports names with a leading slash, which is what the optional `/` absorbs.
- **`labels.type` must stay `model-hotel` or `front-desk`.** CrowdSec's stock
  `crowdsecurity/non-syslog` parser copies the acquisition's `type` label into `evt.Parsed.program`,
  and that is what this parser filters on. Renaming the type silently stops everything downstream.

### Restart and verify

```bash
docker restart <crowdsec-container>        # or: systemctl reload crowdsec
```

The parser and scenarios should show as `enabled,local`:

```bash
cscli parsers list
cscli scenarios list
```

```
 hugalafutro/model-hotel-logs        🏠  enabled,local   /etc/crowdsec/parsers/s01-parse/model-hotel-logs.yaml
 hugalafutro/model-hotel-admin-bf    🏠  enabled,local   /etc/crowdsec/scenarios/model-hotel-admin-bf.yaml
 hugalafutro/model-hotel-throttled   🏠  enabled,local   /etc/crowdsec/scenarios/model-hotel-throttled.yaml
 hugalafutro/model-hotel-vk-bf       🏠  enabled,local   /etc/crowdsec/scenarios/model-hotel-vk-bf.yaml
```

Confirm the acquisition is attached and lines are arriving:

```bash
cscli metrics show acquisition parsers scenarios
```

The acquisition table should list one row per matched container with a non-zero read count. If the
count is zero, the container regexp does not match or the socket is not mounted. If lines are read
but `hugalafutro/model-hotel-logs` shows only `parsed: 0`, the `type` label is wrong.

Finally, push a single line through the whole pipeline without touching the running gateway:

```bash
cscli explain --type model-hotel -v \
  --log '2026/08/18 04:15:02 level=WARNING auth: admin request with invalid token remote_addr=203.0.113.7 path=/api/providers'
```

A healthy run ends with `parser success 🟢` and names the scenario the event landed in:

```
├-------- parser success 🟢
├ Scenarios
    └ 🟢 hugalafutro/model-hotel-admin-bf
```

Substitute a line from your own `docker logs` output to check the shape your instance actually
emits. `--type model-hotel` is what stands in for the acquisition's `type` label here.

## Start in simulation

Simulation runs the buckets and records the overflows as alerts, but produces no decisions, so
nothing is banned while you find out what your normal traffic looks like.

```bash
cscli simulation enable hugalafutro/model-hotel-vk-bf
cscli simulation enable hugalafutro/model-hotel-admin-bf
cscli simulation enable hugalafutro/model-hotel-throttled
cscli simulation status
```

Let it run for a day or two of representative traffic, then read what would have happened:

```bash
cscli alerts list
cscli alerts list --scenario hugalafutro/model-hotel-vk-bf
cscli decisions list                       # simulated entries are prefixed (simul)
```

A simulated overflow is recorded and listed like any other, with `(simul)` in front of it:
`(simul)ban:1` in the `decisions` column of `cscli alerts list`, `(simul)ban` as the action in
`cscli decisions list`. It is the prefix, not an empty list, that tells you nothing is being acted
on. A decision carrying it is never served to a bouncer.

Check every address that shows up. A monitoring probe, a CI job with a stale key, or your own
reverse proxy appearing here is the signal to fix the cause before arming anything. When the list
holds only addresses you are happy to ban:

```bash
cscli simulation disable hugalafutro/model-hotel-vk-bf
cscli simulation disable hugalafutro/model-hotel-admin-bf
cscli simulation disable hugalafutro/model-hotel-throttled
```

## Read this before arming: `TRUSTED_PROXIES`

**A wrong `TRUSTED_PROXIES` gets your own reverse proxy banned, and every service behind it goes
off the internet at once.**

Behind a proxy, Model Hotel resolves the real client address only when the proxy's own address is
listed in `TRUSTED_PROXIES`. When it is not, `X-Forwarded-For` is ignored (correctly, since an
untrusted peer must not be able to dictate the address) and every log line reports the proxy's
address instead. Every failure from every visitor then groups under one address, the bucket
overflows within minutes, and the bouncer bans the proxy.

`crowdsecurity/whitelists` saves you when the proxy sits on a private range. It does not when the
proxy has a public address, or when it reaches the gateway from outside the whitelisted ranges.

### Verify what the app actually logs

Do this once, before you disable simulation. Do not infer it from the config.

1. From a machine whose public address you know (a phone on mobile data works well), make one
   request that fails authentication:

   ```bash
   curl -s -o /dev/null -w '%{http_code}\n' \
     -H 'Authorization: Bearer sk-definitely-not-a-real-key' \
     -H 'Content-Type: application/json' \
     -d '{"model":"nope","messages":[]}' \
     https://your-gateway.example.com/v1/chat/completions
   ```

2. Read the line it produced:

   ```bash
   docker logs --tail 20 <model-hotel-container> 2>&1 | grep 'auth:'
   ```

3. Compare the address in the line against the client's real address.

   ```
   2026/08/18 04:15:02 level=WARNING auth: key not found remote_addr=203.0.113.7 path=/v1/chat/completions
   ```

   - `remote_addr` is the **visitor's** address: correct, carry on.
   - `remote_addr` is your **proxy's** address: stop. Set `TRUSTED_PROXIES` to the proxy's CIDR
     (see the Configuration page in the wiki), restart the gateway, and repeat from step 1.

An instance older than **v0.9.99** logs the peer address with the TCP port attached and without
trusted-proxy resolution, so behind a proxy the source address is unusable no matter what
`TRUSTED_PROXIES` says. Update the instance before arming the scenarios. The parser strips the port
either way, so an old build produces a plausible-looking address that is simply the wrong one.

## Prove it bans before you trust it

An armed engine and a broken pipeline look identical from the outside: both leave
`cscli decisions list` empty on a gateway nobody is attacking. Replay a fixture through the
**running** engine to watch a bucket overflow become a real decision, rather than waiting for an
attacker to answer the question for you.

Use an address from a reserved documentation range (`198.51.100.0/24`, TEST-NET-2). No real client
can hold one, so the ban it earns costs nothing.

```bash
# inside the security engine container
for i in 1 2 3 4 5 6 7 8; do
  echo "$(date +'%Y/%m/%d %H:%M:%S') level=WARNING auth: key not found remote_addr=198.51.100.42"
done > /tmp/vk-bf-test.log

crowdsec -dsn file:///tmp/vk-bf-test.log -type model-hotel -no-api
```

`-no-api` is not optional. Without it the replay starts a second local API, which cannot have the
port the running one already holds:

```
level=fatal msg="local API server stopped with error: listening on 0.0.0.0:8080: bind: address already in use"
```

With it, the replay reports to the API that is already running, which is what writes the decision to
the database the bouncer reads. A run that works announces the overflow:

```
level=info msg="Ip 198.51.100.42 performed 'hugalafutro/model-hotel-vk-bf' (6 events over 0s)"
```

Confirm the alert carried a decision, then take it back out:

```bash
cscli alerts list --ip 198.51.100.42        # reason model-hotel-vk-bf, decisions ban:1
cscli decisions list --ip 198.51.100.42     # action ban
cscli decisions delete --ip 198.51.100.42
rm /tmp/vk-bf-test.log
```

Read the failures as follows:

- An alert or a decision printed as `(simul)ban` means the scenario is still simulated, and no
  bouncer will act on it. `cscli simulation status` names the ones that are.
- No alert, but the replay printed the overflow line: the engine you replayed against is not the
  one the bouncer subscribes to.
- No overflow line at all: the events never classified. Return to `cscli explain` on a single line
  before changing anything else.

The other two scenarios are exercised the same way, with an admin-token or a throttling line in
place of the key failure. `tests/` holds a ready fixture per scenario, whose timestamps are fixed in
the past, which is why the loop above stamps fresh ones instead of replaying a fixture verbatim.

`cscli hubtest` is not a substitute for this. The official container carries no hub index, so
`cscli hubtest list` fails with `unable to read index file: open /.index.json`; the fixtures under
`tests/` are meant for a machine with a full hub checkout, and they prove the scenarios, not the
deployment.

## The opt-in access-log parser

`parsers/s01-parse/model-hotel-access-logs.yaml` maps Model Hotel's own request log onto CrowdSec's
generic HTTP access-log format (`evt.Meta.log_type = http_access-log`, plus `http_status`,
`http_verb`, `http_path`, `target_fqdn`). Installing it lets `crowdsecurity/http-logs` and every
generic `http-*` scenario (path traversal, probing, bad user agents, 4xx flooding) run against
gateway traffic.

It is **not** in the collection and is not installed by the steps above. Install it only when Model
Hotel is exposed directly, with no reverse proxy in front.

**Do not install it behind a proxy whose logs CrowdSec already reads.** The same request is then
parsed twice, once from the proxy's access log and once from the gateway's, so it fills two buckets
and one visitor earns two alerts and two decisions. Pick the log CrowdSec already has, which is
almost always the proxy's, because it sees requests the gateway rejects at the edge as well.

Note also that a stock instance writes only 4xx and 5xx lines to the container log, so any scenario
that counts successful requests sees nothing through this parser.

It follows the same two rules as the main parser: it recognises an access line by an anchored
capture rather than by searching the line, and it reads every field from the first bare token of
that name, so a path cannot forge a method, a status or an address.
`contrib/crowdsec/tests/model-hotel-access-logs` covers that, including a line whose path holds a
complete fake access record.

```bash
cp parsers/s01-parse/model-hotel-access-logs.yaml /etc/crowdsec/parsers/s01-parse/
cscli collections install crowdsecurity/base-http-scenarios   # pulls crowdsecurity/http-logs
docker restart <crowdsec-container>
```

## What is not covered

- **Only warning and error records reach the container log.** With `DEBUG_LOG=false` (the default)
  info and debug records never reach stderr; they still land in the App Logs page and in Postgres.
  Every authentication failure is logged at warn or error, so the security signal is complete, but
  successful requests are invisible to CrowdSec. `DEBUG_LOG=true` would expose them at a cost of
  roughly six extra lines per request, which is not a trade worth making for this.

- **Front Desk coverage is partial.** Front Desk has no access logger, and its session gate returns
  401 without logging anything. What does reach CrowdSec from Front Desk is metrics-scrape failures
  with a bad or missing token, TOTP and OIDC failures, and IP throttling. Ordinary rejected session
  cookies do not.

- **Passkey failures carry no client address.** They are logged, but with no IP in the record there
  is nothing for a scenario to group on, so they cannot produce a decision.

- **Nothing here reads the database.** The App Logs page holds more than the container log does,
  and it is not an acquisition source. If a failure only appears in App Logs, no scenario will see
  it.

## How it works

Anyone writing their own scenario over these events needs three things.

### The three log shapes

The parser normalises all three before classifying anything, so a scenario never has to care which
one an instance emits.

1. **Model Hotel text**, the default, written to stderr. Local time, no timezone, no `T`, level
   word spelled out, unquoted `k=v` attributes:

   ```
   2026/08/18 04:15:02 level=WARNING auth: admin request with invalid token remote_addr=203.0.113.7 path=/api/providers
   ```

2. **`LOG_FORMAT=json`**, on both binaries. One flat JSON object per line, keys alphabetical, with
   the scope lifted out of the message into `source`:

   ```json
   {"level":"warning","msg":"admin request with invalid token","path":"/api/providers","remote_addr":"203.0.113.7","source":"auth","time":"2026-08-18T04:15:02.123456789Z"}
   ```

3. **Front Desk text** (stdlib slog `TextHandler`, written to stdout), which quotes the whole
   `scope: message` into `msg=`:

   ```
   time=2026-08-18T04:15:02.123Z level=WARN msg="frontdesk: metrics scrape with invalid token" remote_addr=203.0.113.7
   ```

### Why a request path cannot ban a stranger

A request path is written by whoever made the request, and it is logged as an attribute. Left
alone, that is enough to drive a log reader: ask for `/api/x%20auth:%20key%20not%20found%20remote_addr=203.0.113.9`
and the resulting line contains a complete, genuine-looking authentication failure attributed to an
address of your choosing. Repeat it six times and an unprotected setup bans a stranger, with no
credentials needed.

Three rules make that inert, and they are why the parser is written the way it is:

1. **Classification reads the message only.** `mh_body` is captured by position: it is whatever
   follows `level=<LEVEL> `, and every filter is anchored to the start of it with `startsWith`.
   A copy of a message that appears further along the line, inside an attribute value, matches
   nothing. This is also why you will not find `contains` anywhere in the classification.
2. **The address is the first token of an address name, and is validated only after it is taken.**
   Taking the first and then requiring it to parse, rather than scanning for the first thing that
   looks like an address, means a line whose real address is malformed yields nothing instead of
   falling through to whatever a caller wrote further along.
3. **A line that names the address twice is refused.** No call site in the gateway logs the
   address more than once, so a second occurrence means a `key=value` pair came out of a value.
   Such an event is left with no `source_ip`, and every scenario requires one, so it cannot reach
   a bucket.

From v0.9.99 the gateway also escapes attribute values, and the important half is that it escapes
the spaces inside them: a quoted value that still contained ` remote_addr=203.0.113.9 ` would
remain a `key=value` token to a reader that splits on whitespace before it considers quotes, which
is what a grok, a fail2ban regex and an awk one-liner all do. Escaped, a value is one token and
cannot present a pair at all. That protects everything else reading these logs, not just CrowdSec.

Rules 1 and 3 hold without the escaping, so the collection is safe to point at an older instance.
One gap remains there and it is why v0.9.99 is listed as a requirement: on an older build, a value
logged *before* the address on the same line can still supply the first address token. Rule 3
catches every case where the real address is also present, which is all of them in current code,
but a build old enough to log a caller-controlled value ahead of the address is relying on rule 3
alone rather than on two independent defences.

`contrib/crowdsec/tests/model-hotel-logs` pins all of this. It feeds both the escaped and the bare
form of an injected access line and asserts neither is classified; a real auth failure carrying an
injected address in an escaped value and asserts the real client survives; and the same line in the
bare form and asserts it resolves to no address at all rather than the wrong one.

### The four names for the client address

The address arrives under a different key depending on the call site. The parser tries them in this
order and takes the first non-empty one:

| Key | Written by |
|---|---|
| `remote_addr` | authentication and login lines |
| `remote` | the access log |
| `client_ip` | the proxy |
| `ip` | the IP rate limiter |

Builds older than v0.9.99 attach the TCP port. The parser strips an optional port and any IPv6
brackets, so `evt.Meta.source_ip` is always a bare address.

### The meta contract

| Field | Value |
|---|---|
| `evt.Meta.service` | `model-hotel` |
| `evt.Meta.log_type` | `model-hotel_auth_fail` or `model-hotel_throttled` |
| `evt.Meta.sub_type` | on `auth_fail` only: `vk_invalid`, `admin_token`, `login`, `sso`, `csrf`, `forbidden`, `backup_signature` |
| `evt.Meta.source_ip` | the client address, port and IPv6 brackets stripped |
| `evt.Meta.log_format` | `text` or `json` |
| `evt.Meta.http_path` | the request path, where the line carries one |
| `evt.Meta.target_user` | the username, on `forbidden` |

Only recognised records advance past `s01-parse`; everything else is dropped there, so the rest of
the pipeline never sees unrelated gateway chatter. A scenario of your own should filter on
`log_type` and `sub_type` and group by `source_ip`, the way the three shipped ones do:

```yaml
filter: "evt.Meta.log_type == 'model-hotel_auth_fail' && evt.Meta.sub_type == 'sso' && evt.Meta.source_ip != ''"
groupby: evt.Meta.source_ip
```

### Sparing a client you trust

`model-hotel-vk-bf` overflows on the sixth rejected key inside roughly 100 seconds, and the stock
remediation is a 4 hour ban. An OpenAI-compatible client with a revoked key and a default retry
policy can reach that in two user actions, so a known-good caller with stale credentials is worth
excluding outright. A whitelist in `s02-enrich` does it:

```yaml
# /etc/crowdsec/parsers/s02-enrich/model-hotel-whitelist.yaml
name: yourname/model-hotel-whitelist
description: "Never act on the office range or the CI runner"
whitelist:
    reason: "trusted Model Hotel clients"
    ip:
        - "203.0.113.10"
    cidr:
        - "198.51.100.0/24"
```

Whitelisting beats widening the thresholds: the bucket still fills and the alert still appears in
`cscli alerts list`, so you keep the visibility and lose only the ban.

## Submitting to the hub

The items are namespaced `hugalafutro/` and this directory mirrors the CrowdSec hub layout
(`parsers/s01-parse/`, `scenarios/`, `collections/`), so submitting them upstream is a copy of the
tree into a hub fork plus the hubtest fixtures under `tests/`. Write those with `cscli hubtest
create` and run them with `cscli hubtest run`; the log samples in the section above are the
starting material for the fixtures.

`collections/model-hotel.yaml` lists `hugalafutro/model-hotel-logs` and the three scenarios. The
access-log parser is deliberately left out, for the double-counting reason above: a collection
should be safe to install behind a proxy.
