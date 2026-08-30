# Hubtest fixtures

These five directories are `cscli hubtest` fixtures for the Model Hotel collection that lives one
level up. They are the regression suite the CrowdSec hub runs against a submitted parser and its
scenarios, and they are the fastest way to prove locally that a change to
`parsers/s01-parse/model-hotel-logs.yaml` or to any of `scenarios/*.yaml` still does what it says.

Each directory holds the four files a hubtest is made of:

| file              | what it is                                                              |
| ----------------- | ----------------------------------------------------------------------- |
| `config.yaml`     | which parsers and scenarios to load, the log file, and its `log_type`    |
| `<name>.log`      | the input lines                                                          |
| `parser.assert`   | expr assertions over the per stage parser results                        |
| `scenario.assert` | expr assertions over the overflows the scenarios produced                |

One of the two assert files is empty in every test, the same way the upstream reference tests
(`.tests/authelia-logs` and `.tests/authelia-bf`) do it: a parser test asserts nothing about
overflows, and a scenario test sets `ignore_parsers: true` and asserts nothing about parsing.

## What each test covers

`model-hotel-logs` is the parser test. Its 31 lines carry one example of every `sub_type` the
parser can emit (`vk_invalid`, `admin_token`, `login`, `sso`, `csrf`, `forbidden`,
`backup_signature`), three throttling lines, all three log shapes Model Hotel and Front Desk emit
(Model Hotel text, `LOG_FORMAT=json`, Front Desk slog text), one address that still carries a TCP
port the way pre-#674 builds logged it, and one info level line the parser has to refuse. The
asserts pin `log_type`, `sub_type`, `source_ip`, `log_format`, `service`, and where the line
carries them `http_path` and `target_user`, per line. The last line, `auth: authenticated`,
asserts `Success == false` in `s01-parse`: it reaches the parser and is dropped there, which is
what keeps successful requests out of the buckets.

The last six lines are the log-injection regression, and they come in pairs: the escaped form a
v0.9.99 instance emits, and the bare form an older build does. Two are access lines whose request
path holds a complete copy of an authentication failure plus an attacker-chosen `remote_addr`, and
neither may classify. Two are real auth failures whose path holds an injected address; the escaped
one must still resolve to the real client, and the bare one must resolve to no address at all
rather than the wrong one. The last two do the same through a virtual key name, which is
caller-chosen and permits spaces, and which sits on the one classified line that logs another
attribute beside the address.

Between them they pin all three defences. Rewrite a filter with `contains` and the access lines
classify. Scan for the first thing that looks like an address instead of taking the first address
token and the bare auth line picks the attacker's. Drop the duplicate-address check and the bare
lines start naming strangers.

The last six lines are Front Desk's own, in its slog framing: an access record, the two admin-gate
rejections and the CSRF rejection its control plane emits, a rejected passkey assertion, and one
admin rejection whose path carries an injected address. They pin that Front Desk reaches the same
`admin_token`, `csrf` and `login` buckets as the gateway with the same messages, that its access
record is refused by the main parser (it belongs to the opt-in access parser instead), and that its
escaped path still resolves to the real client.

`model-hotel-access-logs` covers the opt-in parser that is deliberately left out of the collection.
It pins the mapping onto the generic `http_access-log` contract for all three shapes (Model Hotel
text, JSON, Front Desk slog text), a pre-#674 address that still carries its TCP port, and the same
injection case in both framings: an auth line whose path holds a fake access record must not become
an access log, and an access line whose path holds an injected address must still name the real
client.

`model-hotel-vk-bf`, `model-hotel-admin-bf` and `model-hotel-throttled` are the scenario tests.
Each pours enough lines from a single source address to overflow its bucket exactly once and
asserts the scenario name, the source, the event count and the metadata on every event in the
alert. The admin test deliberately mixes `admin_token`, `login` and `csrf` lines, which is how it
proves those three sub types share one bucket rather than filling three.

## Running them

`cscli hubtest` needs a hub checkout, because `config.yaml` resolves the collection's own items by
a path relative to the hub root.

```bash
git clone https://github.com/crowdsecurity/hub.git
cd hub
mkdir -p parsers/s01-parse/hugalafutro scenarios/hugalafutro collections/hugalafutro
cp <model-hotel>/contrib/crowdsec/parsers/s01-parse/model-hotel-logs.yaml parsers/s01-parse/hugalafutro/
cp <model-hotel>/contrib/crowdsec/scenarios/*.yaml                        scenarios/hugalafutro/
cp <model-hotel>/contrib/crowdsec/collections/model-hotel.yaml            collections/hugalafutro/
cp -r <model-hotel>/contrib/crowdsec/tests/model-hotel-* .tests/

cscli hubtest run model-hotel-logs model-hotel-vk-bf model-hotel-admin-bf model-hotel-throttled \
  --hub . --report-success
```

Once the items are merged into the hub, `.tests/` is where they already live and only the last
command is needed.

A failing assert prints the expression and the value it actually saw. To rebuild an assert file
from scratch after an intentional parser change, empty it and run the test again: `cscli hubtest`
prints a complete generated assert file to stdout when the file is empty, and it exits non zero so
it cannot be mistaken for a pass. The files here are that output, trimmed to the fields that
matter and with the `timestamp` assertions removed, because those depend on the machine's clock
handling rather than on the parser.

## The fast local check

Rebuilding a hub checkout for every edit is slow. A container with the parser and scenarios
installed under `/etc/crowdsec` answers the same question in a second:

```bash
# does every line classify the way the asserts claim?
docker exec -i <crowdsec> sh -c 'cat > /tmp/t.log' < model-hotel-logs/model-hotel-logs.log
docker exec <crowdsec> cscli explain --file /tmp/t.log --type model-hotel -v

# does the bucket overflow, once, on the right source?
docker exec -i <crowdsec> sh -c 'cat > /tmp/t.log' < model-hotel-vk-bf/model-hotel-vk-bf.log
docker exec <crowdsec> crowdsec -dsn file:///tmp/t.log -type model-hotel -no-api
```

`cscli explain` prints its lines in a non deterministic order, so match them by content and not by
position. `crowdsec -dsn` prints one `Ip <addr> performed '<scenario>' (N events over Ns)` line per
overflow, which is the ground truth a `scenario.assert` has to agree with; `-no-api` is required,
without it the process tries to bind port 8080 and dies.

## Rules the fixtures follow

Addresses come only from the documentation ranges `203.0.113.0/24`, `198.51.100.0/24` and
`192.0.2.0/24`. A private address would be dropped by `crowdsecurity/whitelists` before it reached
a bucket, and the test would fail in a way that looks like a parser bug.

Log lines are copied byte for byte from what the two binaries actually write. The Model Hotel text
shape uses local time `2006/01/02 15:04:05` with no zone and no `T`, and its level word is
`WARNING` or `ERROR`, never `WARN`. Front Desk uses stdlib slog, so it is `WARN` and an RFC3339
`time=`. Attribute values are never quoted on the Model Hotel side, which is why
`reason=nonce mismatch` appears unquoted and why only `nonce` survives into `Unmarshaled`.

Nothing is asserted that changes between runs. The line timestamps are fixed in the fixtures and
drive the leaky buckets, but no assertion reads a wall clock value.
